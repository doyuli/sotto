package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/naming"
	"sotto/internal/ocr"
	"sotto/internal/selection"
	"sotto/internal/translate"
)

// The services the pages call. mygo generate turns them into the typed
// client in src/mygo.ts: ConfigSvc.Get becomes ConfigSvc.get.

// ConfigSvc reads and writes the settings.
type ConfigSvc struct{ app *application }

func (s ConfigSvc) Get() config.Settings { return s.app.settings() }

// Set normalizes, saves and applies the settings.
func (s ConfigSvc) Set(settings config.Settings) error {
	return s.app.updateSettings(settings)
}

// SetSecret stores a credential; an empty value deletes it.
func (s ConfigSvc) SetSecret(name, value string) error {
	if !slices.Contains(keys.Names, name) {
		return errors.New("unknown credential: " + name)
	}
	s.app.configWriteMu.Lock()
	defer s.app.configWriteMu.Unlock()

	// Disable services that would lose a required credential before removing
	// it, so a failed settings write cannot leave an enabled service behind.
	if value == "" {
		cfg := s.app.settings()
		credentials := s.app.keys.Snapshot()
		delete(credentials, name)
		enabled := translate.Available(cfg, credentials)
		if !slices.Equal(enabled, cfg.Services.Enabled) {
			cfg.Services.Enabled = enabled
			if err := s.app.saveSettings(cfg); err != nil {
				return err
			}
		}
	} else if value = strings.TrimSpace(value); value == "" {
		return errors.New("请输入凭证")
	}
	if err := s.app.keys.Set(name, value); err != nil {
		return fmt.Errorf("更新凭证：%w", err)
	}
	return nil
}

// CredentialStatus contains presence information without exposing stored values.
type CredentialStatus struct {
	Stored     []string `json:"stored"`
	Configured []string `json:"configured"`
}

// Credentials reports which credentials are stored and which services can be enabled.
func (s ConfigSvc) Credentials() CredentialStatus {
	credentials := s.app.keys.Snapshot()
	status := CredentialStatus{Stored: []string{}, Configured: translate.Configured(s.app.settings(), credentials)}
	for _, name := range keys.Names {
		if credentials.Get(name) != "" {
			status.Stored = append(status.Stored, name)
		}
	}
	return status
}

// TestService returns a user-facing error, or an empty string on success.
func (s ConfigSvc) TestService(ctx context.Context, name string) string {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return translate.UserMessage(translate.Test(ctx, name, s.app.settings(), s.app.keys.Snapshot()))
}

// serviceKeyPages maps a service to the console page where its
// credentials are issued. The whitelist keeps the pages from opening
// anything but these addresses.
var serviceKeyPages = map[string]string{
	"deepl":      "https://www.deepl.com/zh/pro-api",
	"openai":     "https://platform.openai.com/api-keys",
	"baidu":      "https://fanyi-api.baidu.com/manage/repository",
	"tencent":    "https://console.cloud.tencent.com/cam/capi",
	"youdao":     "https://ai.youdao.com/console",
	"azure":      "https://portal.azure.com/#create/Microsoft.CognitiveServicesTextTranslation",
	"volcengine": "https://console.volcengine.com/iam/keymanage/",
}

// OpenKeyPage opens the service's credential page in the default browser.
func (ConfigSvc) OpenKeyPage(service string) error {
	page, ok := serviceKeyPages[service]
	if !ok {
		return fmt.Errorf("%s 没有需要配置的密钥", service)
	}
	return openInBrowser(page)
}

// DeeplUsage is DeepL's monthly character count and limit.
type DeeplUsage struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// DeeplUsage reports this month's DeepL character count and limit.
func (ConfigSvc) DeeplUsage() (DeeplUsage, error) {
	used, limit, err := translate.DeepL{}.Usage(context.Background(), keys.Store{}.Get(keys.DeeplKey))
	return DeeplUsage{Used: used, Limit: limit}, err
}

// OcrLanguages lists the languages the installed OCR engine recognizes.
func (ConfigSvc) OcrLanguages() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ocr.AvailableLanguages(ctx)
}

// TranslateSvc runs translations for the pages.
type TranslateSvc struct{ app *application }

// Run translates req and pushes the results to the calling window as
// translation-update events, one per service as it arrives.
func (s TranslateSvc) Run(ctx context.Context, req RunReq) error {
	win := mygo.CallerWindow(ctx)
	if win == nil {
		return errors.New("no calling window")
	}
	if req.Text == "" {
		return nil
	}
	s.app.runTranslation(win, req)
	return nil
}

// Cancel stops a run; the next one cancels the last on its own, so this
// is for the pages' explicit stop.
func (s TranslateSvc) Cancel(ctx context.Context, sessionID string, revision int64) {
	s.app.bubbles.Cancel(mygo.CallerWindow(ctx), sessionID, revision)
}

// FormatOption is one naming style of a translation, ready to copy.
type FormatOption struct {
	Style string `json:"style"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// Formats turns an English translation into the configured naming
// styles, for the bubble's copy chips.
func (s TranslateSvc) Formats(text string) []FormatOption {
	words := naming.SplitWords(text)
	var out []FormatOption
	for _, style := range s.app.settings().CopyFormats {
		out = append(out, FormatOption{
			Style: style,
			Label: naming.Labels[style],
			Value: naming.Format(words, style),
		})
	}
	return out
}

// BubbleSvc serves the bubble pages.
type BubbleSvc struct{ app *application }

// Ready returns the bubble's state. Its page asks on mounting, because
// events sent before its DOM is ready are held only in small numbers.
func (s BubbleSvc) Ready(id int64) (BubbleState, error) {
	state, ok := s.app.bubbles.State(s.app.settings(), id)
	if !ok {
		return BubbleState{}, errors.New("no such bubble")
	}
	return state, nil
}

// Pin keeps the bubble on screen when it loses focus.
func (s BubbleSvc) Pin(id int64) { s.app.bubbles.Pin(id) }

// Unpin returns a pinned bubble to hiding on blur.
func (s BubbleSvc) Unpin(id int64) { s.app.bubbles.Unpin(s.app.settings(), id) }

// Close hides an unpinned bubble and closes a pinned one.
func (s BubbleSvc) Close(id int64, sessionID string, showID, revision int64) {
	s.app.bubbles.Close(id, sessionID, showID, revision)
}

// OpenTranslationSettings opens the services tab from the bubble.
func (s BubbleSvc) OpenTranslationSettings() {
	mygo.RunOnMain(func() { s.app.showSettings(true) })
}

// Copy puts text on the clipboard.
func (BubbleSvc) Copy(text string) { mygo.Clipboard.WriteText(text) }

// HotkeySvc manages the global shortcuts.
type HotkeySvc struct{ app *application }

// Apply switches to these shortcuts, rolling back on failure.
func (s HotkeySvc) Apply(hotkeys config.HotkeySettings) error {
	return s.app.applyHotkeys(hotkeys)
}

// Busy reports whether an accelerator is taken, by any app.
func (HotkeySvc) Busy(accel string) bool {
	return mygo.GlobalShortcut.IsRegistered(accel)
}

// SnipSvc serves the screenshot selection page.
type SnipSvc struct{ app *application }

// Done closes a shot: rect is the dragged rectangle in client
// coordinates, nil for a cancel.
func (s SnipSvc) Done(ctx context.Context, sessionID int64, rect *snipRect) {
	s.app.snipDone(mygo.CallerWindow(ctx), sessionID, rect)
}

// Start opens a new screenshot session from a recovery action.
func (s SnipSvc) Start() { mygo.RunOnMain(func() { s.app.startEntry("ocr") }) }

// RestoreClipboard retries a retained snapshot without replacing a newer copy.
func (BubbleSvc) RestoreClipboard(id string) error {
	err := selection.RecoverClipboard(id)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, selection.ErrSourceChanged):
		return errors.New("剪贴板已变化，未覆盖新内容；可放弃恢复副本后继续取词。")
	case errors.Is(err, selection.ErrClipboardRecoveryGone):
		return errors.New("恢复副本已处理，请重新打开面板。")
	default:
		log.Printf("clipboard recovery: %v", err)
		return errors.New("剪贴板恢复失败，副本仍保留，可稍后重试。")
	}
}

func (BubbleSvc) DiscardClipboardRecovery(id string) { selection.DiscardClipboardRecovery(id) }
