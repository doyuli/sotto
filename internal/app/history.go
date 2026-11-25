package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/history"
	"sotto/internal/platform"
	"sotto/internal/selection"
	"sotto/internal/translate"
	"sotto/internal/ui/history"
	"sotto/internal/usermessage"
)

const historyMinWidth, historyMinHeight = 560, 360

func historyPolicy(cfg config.HistorySettings) history.Policy {
	return history.Policy{Enabled: cfg.Enabled, Days: cfg.RetentionDays}
}

func (a *application) startHistory(cfg config.HistorySettings) {
	policy := historyPolicy(cfg)
	if a.fallbackSettings {
		// A request begun before we know the user's recording preference must stay unrecorded.
		policy = history.Policy{}
	}
	s := history.New(policy, time.Now)
	a.history = s
	ctx, cancel := context.WithCancel(context.Background())
	a.historyCtx, a.stopHistory = ctx, cancel
	go func() {
		if err := a.initializeHistory(ctx); err != nil && ctx.Err() == nil {
			log.Printf("translation history unavailable: %v", err)
		}
		timer := time.NewTicker(24 * time.Hour)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.Changes():
				mygo.RunOnMain(func() {
					if a.refreshHistory != nil {
						a.refreshHistory()
					}
				})
			case <-timer.C:
				if err := s.Cleanup(ctx); err != nil && !errors.Is(err, history.ErrUnavailable) && ctx.Err() == nil {
					log.Print("translation history: scheduled cleanup failed")
				}
			}
		}
	}()
}

func (a *application) initializeHistory(ctx context.Context) error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	if a.history.IsOpen() {
		return nil
	}
	if !a.fallbackSettings {
		return a.history.Initialize(ctx, filepath.Dir(a.cfgPath), historyPolicy(a.settings().History))
	}
	// Recover the database under its own retention and recording policy, never the fallback defaults.
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return fmt.Errorf("locate history settings: %w", err)
	}
	recovered, err := config.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		return fmt.Errorf("load history settings: %w", err)
	}
	if err := a.history.Initialize(ctx, dir, historyPolicy(recovered.History)); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg.History = recovered.History
	cfg, view := a.cfg, a.settingsView
	a.mu.Unlock()
	if view != nil {
		view.ApplyExternalSettings(cfg)
	}
	return nil
}

func (a *application) closeHistory() {
	if a.stopHistory != nil {
		a.stopHistory()
	}
	if a.history != nil {
		if err := a.history.Close(); err != nil {
			log.Print("translation history: could not close database")
		}
	}
}

func (a *application) updateHistorySettings(settings config.HistorySettings, approved int64) error {
	if a.fallbackSettings {
		return usermessage.New("本次启动未能读取用户设置，请重启后再修改历史设置")
	}
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	cfg := a.settings()
	cfg.History = settings
	saved := false
	err := a.history.UpdatePolicy(context.Background(), historyPolicy(settings), approved, func() error {
		if err := config.Save(a.cfgPath, cfg); err != nil {
			return fmt.Errorf("save translation history settings: %w", err)
		}
		saved = true
		return nil
	})
	if saved {
		a.applySettings(cfg)
	}
	return err
}

func (a *application) beginHistory(original string, request translate.Request, order []string) *history.Request {
	if a.history == nil {
		return nil
	}
	return a.history.Begin(history.Key{Text: original, SourceLang: request.SourceLang, TargetLang: request.TargetLang}, order)
}

func (a *application) recordTranslation(ctx context.Context, request *history.Request, result translate.Result) {
	if request == nil || ctx.Err() != nil {
		return
	}
	text, resultType := completedHistoryResult(result)
	if _, err := a.history.Record(ctx, request, result.Service, text, resultType); err != nil &&
		!errors.Is(err, history.ErrUnavailable) && ctx.Err() == nil {
		log.Print("translation history: could not save completed translation")
	}
}

func completedHistoryResult(result translate.Result) (string, history.ResultType) {
	if result.Streaming || result.Err != "" || strings.TrimSpace(result.Text) == "" {
		return "", 0
	}
	if entry := result.Dictionary; entry != nil {
		text := result.Text
		// The provider's plain result only carries one accent. Preserve all labeled pronunciations for history.
		var accents []string
		if entry.UKPhonetic != "" {
			accents = append(accents, "英 /"+entry.UKPhonetic+"/")
		}
		if entry.USPhonetic != "" {
			accents = append(accents, "美 /"+entry.USPhonetic+"/")
		}
		if len(accents) > 0 {
			text = strings.Join(accents, "  ") + "\n" + entry.Meanings
		}
		return entry.CopyText(text), history.ResultDictionary
	}
	return result.Text, history.ResultTranslation
}

func (a *application) toggleHistory() {
	a.toggleHistoryWindow(a.historyWin != nil && a.historyWin.IsVisible(), a.showHistory, a.dismissHistory)
}

func (a *application) toggleHistoryWindow(visible bool, show, dismiss func()) {
	if a.isQuitting() || a.history == nil {
		return
	}
	if visible && dismiss != nil {
		dismiss()
		return
	}
	show()
}

func (a *application) showHistory() {
	if a.isQuitting() || a.history == nil {
		return
	}
	work := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	if a.historyWin != nil {
		a.focusHistory(work)
		return
	}
	var w *mygo.Window
	actions := historyview.Actions{
		OpenStore: a.initializeHistory,
		Post:      func(f func()) { w.Update(f) },
		Copy:      func(text string) error { return mygo.Clipboard.Write(transfer.TextData(text)) },
		Speak: func(text, lang string, onError func()) {
			a.speaker.Speak(text, lang, func(err error) {
				log.Printf("speech: %v", err)
				w.Update(onError)
			})
		},
		Close:        func() { w.Close() },
		OpenServices: func() { a.showSettings(true) },
		Options: func() ([]string, []string) {
			cfg := a.settings()
			return enabledHistoryProviders(cfg.Services), cfg.CopyFormats
		},
		DictionaryEligible: func(entry history.Entry) bool {
			_, err := prepareHistoryRequest(entry, "dictionary", a.settings())
			return err == nil
		},
		Retry: func(entry history.Entry, provider string) (historyview.RetryResult, error) {
			// Translation can finish recording after the transient window closes.
			return a.retranslateHistory(a.historyCtx, entry, provider)
		},
	}
	view := historyview.New(a.history, actions, time.Now)
	w = mygo.NewWindow(platform.SettingsWindow(mygo.WindowOptions{
		Title: "翻译历史", Content: ui.View(view.Render), Width: historyview.DefaultWidth, Height: historyview.DefaultHeight,
		MinWidth: historyMinWidth, MinHeight: historyMinHeight, Hidden: true, SkipTaskbar: true,
		TitleBarStyle: mygo.TitleBarHidden, HideWindowButtons: true,
		StateKey: "translation-history", BackgroundColor: "light-dark(#ffffff, #222325)",
	}))
	a.historyWin = w
	var returnFocus func()
	a.focusHistory = func(work mygo.Rectangle) {
		if !w.IsFocused() {
			returnFocus = platform.HistoryFocusReturn()
		}
		if work.Width > 0 && work.Height > 0 {
			bounds := w.Bounds()
			w.SetMinimumSize(min(historyMinWidth, work.Width), min(historyMinHeight, work.Height))
			if next := historyBoundsForWorkArea(bounds, work); next != bounds {
				w.SetBounds(next)
			}
		}
		view.Open()
		w.Invalidate()
		w.Show()
	}
	a.refreshHistory = func() {
		view.Refresh()
		w.Invalidate()
	}
	a.dismissHistory = view.Dismiss
	dismissOnLeave := func(outsideNonactivatingPanel bool) {
		// Blur can arrive during a native focus transition. Check the current
		// window and any popup it owns when the transition has settled.
		w.Update(func() {
			if a.historyWin != w || !w.IsVisible() {
				return
			}
			if outsideNonactivatingPanel || (!w.IsFocused() && !selection.WindowOwnsForeground(w.NativeHandle())) {
				view.Dismiss()
			}
		})
	}
	w.OnBlur(func() { dismissOnLeave(false) })
	var stopWatching func()
	w.OnShow(func() {
		w.Update(func() {
			if a.historyWin != w || !w.IsVisible() || stopWatching != nil {
				return
			}
			var err error
			stopWatching, err = selection.WatchWindowLeave(w.NativeHandle(), func() {
				// AppKit can leave a nonactivating panel key after an outside click.
				// The monitor has already excluded clicks inside the panel and its menus.
				dismissOnLeave(runtime.GOOS == "darwin")
			})
			if err != nil && !errors.Is(err, selection.ErrUnsupported) {
				log.Printf("watch translation history window: %v", err)
			}
		})
	})
	w.OnClose(func(*mygo.CloseEvent) {
		if !a.isQuitting() && w.IsFocused() && returnFocus != nil {
			returnFocus()
		}
	})
	w.OnClosed(func() {
		view.Dispose()
		a.historyWin, a.focusHistory, a.refreshHistory, a.dismissHistory = nil, nil, nil, nil
		if stopWatching != nil {
			stopWatching()
		}
	})
	a.focusHistory(work)
}

func historyBoundsForWorkArea(bounds, work mygo.Rectangle) mygo.Rectangle {
	if work.Width <= 0 || work.Height <= 0 {
		return bounds
	}
	width := min(max(bounds.Width, historyMinWidth), work.Width)
	height := min(max(bounds.Height, historyMinHeight), work.Height)
	// Keep a dragged position when the saved window fits the invocation display.
	if width == bounds.Width && height == bounds.Height &&
		bounds.X >= work.X && bounds.X+bounds.Width <= work.X+work.Width &&
		bounds.Y >= work.Y && bounds.Y+bounds.Height <= work.Y+work.Height {
		return bounds
	}
	return mygo.Rectangle{
		X: work.X + (work.Width-width)/2, Y: work.Y + (work.Height-height)/2,
		Width: width, Height: height,
	}
}

var knownHistoryProviders = config.Default().Services.Order

func enabledHistoryProviders(services config.ServiceSettings) []string {
	return slices.DeleteFunc(slices.Clone(services.Order), func(id string) bool {
		return !slices.Contains(services.Enabled, id) || !slices.Contains(knownHistoryProviders, id)
	})
}

func prepareHistoryRequest(entry history.Entry, provider string, cfg config.Settings) (translate.Request, error) {
	request, err := prepareTranslationRequest(entry.Text, entry.SourceLang, entry.TargetLang, cfg)
	if err != nil {
		return translate.Request{}, errors.New(translate.UserMessage(err))
	}
	if provider == "dictionary" && !translate.AllowsEnglishLookup(request.Text, request.SourceLang) {
		return translate.Request{}, errors.New("词典仅支持查询单个英文单词")
	}
	return request, nil
}

func (a *application) retranslateHistory(ctx context.Context, selected history.Entry, provider string) (historyview.RetryResult, error) {
	cfg := a.settings()
	if !slices.Contains(enabledHistoryProviders(cfg.Services), provider) {
		return historyview.RetryResult{}, errors.New("请在设置里启用此翻译源后重试")
	}
	entry, record, err := a.history.BeginForEntry(ctx, selected.ID, cfg.Services.Order)
	if err != nil {
		return historyview.RetryResult{}, err
	}
	defer record.Done()
	request, err := prepareHistoryRequest(entry, provider, cfg)
	if err != nil {
		return historyview.RetryResult{}, err
	}
	credentials, err := a.translationCredentials(ctx, &cfg.Services, provider)
	if err != nil {
		return historyview.RetryResult{}, errors.New(translate.UserMessage(err))
	}
	// Settings may have changed while the worker was waiting for credentials.
	if !slices.Contains(a.settings().Services.Enabled, provider) || !slices.Contains(cfg.Services.Enabled, provider) {
		return historyview.RetryResult{}, errors.New("请在设置里启用此翻译源后重试")
	}
	cfg.Services.Enabled = []string{provider}
	var final translate.Result
	translate.Run(ctx, cfg, credentials, request, func(result translate.Result) {
		if !result.Streaming {
			final = result
		}
	})
	if ctx.Err() != nil {
		return historyview.RetryResult{}, ctx.Err()
	}
	if final.Err != "" {
		return historyview.RetryResult{}, errors.New(final.Err)
	}
	text, resultType := completedHistoryResult(final)
	if text == "" {
		if provider == "dictionary" {
			return historyview.RetryResult{}, errors.New("词典没有找到词条，请选择其他翻译源")
		}
		return historyview.RetryResult{}, errors.New("该翻译源未返回完整译文")
	}
	if saved, err := a.history.Record(ctx, record, provider, text, resultType); saved && err == nil {
		return historyview.RetryResult{}, nil
	}
	if _, err := a.history.Entry(ctx, entry.ID); errors.Is(err, history.ErrMissing) {
		return historyview.RetryResult{}, err
	}
	if ctx.Err() != nil {
		return historyview.RetryResult{}, ctx.Err()
	}
	return historyview.RetryResult{Text: text, Type: resultType, Notice: "本次译文未写入翻译历史。"}, nil
}
