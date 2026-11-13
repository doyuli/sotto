package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/translate"
)

type credentialStore interface {
	Get(string) string
	Read(string) (string, error)
	Set(string, string) error
	Snapshot(...string) keys.Snapshot
}

// ConfigSvc reads and writes the settings and credentials for the native settings UI.
type ConfigSvc struct{ app *application }

func (s ConfigSvc) Get() config.Settings { return s.app.settings() }

// Set normalizes, saves and applies the settings.
func (s ConfigSvc) Set(settings config.Settings) error {
	return s.app.updateSettings(settings)
}

// SetCustom commits a custom definition and its credential under one writer lock.
// A nil credential keeps the saved key; a removed definition deletes its key.
func (s ConfigSvc) SetCustom(settings config.Settings, id string, credential *string) error {
	if !config.IsCustomService(id) {
		return errors.New("无效的自定义服务")
	}
	a := s.app
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	service, exists := settings.Services.FindCustom(id)
	settings.Services.Custom = slices.Clone(a.settings().Services.Custom)
	if exists {
		i := slices.IndexFunc(settings.Services.Custom, func(cs config.CustomService) bool { return cs.ID == id })
		if i < 0 {
			settings.Services.Custom = append(settings.Services.Custom, service)
		} else {
			settings.Services.Custom[i] = service
		}
	} else {
		settings.Services.Custom = slices.DeleteFunc(settings.Services.Custom, func(cs config.CustomService) bool { return cs.ID == id })
		empty := ""
		credential = &empty
	}
	settings = config.Normalize(settings)
	settings.Services.Enabled = translate.Available(settings, a.keys)
	name := keys.CustomAPIKey(id)
	if credential == nil {
		if a.blockedCredentials[name] {
			return errors.New("服务已暂停，请重新填写或清除 API Key")
		}
		return a.saveSettings(settings)
	}

	previous, err := a.keys.Read(name)
	if err != nil {
		return fmt.Errorf("读取原凭证：%w", err)
	}
	if err := a.keys.Set(name, strings.TrimSpace(*credential)); err != nil {
		return fmt.Errorf("更新凭证：%w", err)
	}
	if err := a.saveSettings(settings); err != nil {
		if restoreErr := a.keys.Set(name, previous); restoreErr != nil {
			if a.blockedCredentials == nil {
				a.blockedCredentials = make(map[string]bool)
			}
			a.blockedCredentials[name] = true
			return errors.Join(err, fmt.Errorf("恢复原凭证失败，服务已暂停，请重新配置 API Key：%w", restoreErr))
		}
		return err
	}
	delete(a.blockedCredentials, name)
	return nil
}

// SetSecret stores a credential; an empty value deletes it.
func (s ConfigSvc) SetSecret(name, value string) error {
	if !slices.Contains(keys.Names, name) && !keys.IsCustomKey(name) {
		return errors.New("unknown credential: " + name)
	}
	s.app.configWriteMu.Lock()
	defer s.app.configWriteMu.Unlock()

	// Disable services that would lose a required credential before removing
	// it, so a failed settings write cannot leave an enabled service behind.
	if value == "" {
		cfg := s.app.settings()
		credentials := s.app.keysSnapshot(cfg)
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
	delete(s.app.blockedCredentials, name)
	return nil
}

// CredentialStatus contains presence information without exposing stored values.
type CredentialStatus struct {
	Stored     []string `json:"stored"`
	Configured []string `json:"configured"`
}

// Credentials reports which credentials are stored and which services can be enabled.
func (s ConfigSvc) Credentials() CredentialStatus {
	cfg, credentials := s.app.settingsSnapshot()
	status := CredentialStatus{Stored: []string{}, Configured: translate.Configured(cfg, credentials)}
	for name := range credentials {
		status.Stored = append(status.Stored, name)
	}
	slices.Sort(status.Stored)
	return status
}

// TestService returns a user-facing error, or an empty string on success.
func (s ConfigSvc) TestService(ctx context.Context, name string) string {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cfg, credentials := s.app.settingsSnapshot()
	if config.IsCustomService(name) {
		if _, ok := cfg.Services.FindCustom(name); !ok {
			return "该服务当前不可用，请重新配置 API Key"
		}
	}
	return translate.UserMessage(translate.Test(ctx, name, cfg, credentials))
}

// serviceKeyPages maps a service to the console page where its
// credentials are issued. The whitelist keeps the pages from opening
// anything but these addresses.
var serviceKeyPages = map[string]string{
	"deepl":       "https://www.deepl.com/zh/pro-api",
	"openai":      "https://platform.openai.com/api-keys",
	"siliconflow": "https://cloud.siliconflow.cn/account/ak",
	"zhipu":       "https://bigmodel.cn/usercenter/proj-mgmt/apikeys",
	"baidu":       "https://fanyi-api.baidu.com/manage/repository",
	"tencent":     "https://console.cloud.tencent.com/cam/capi",
	"youdao":      "https://ai.youdao.com/console",
	"azure":       "https://portal.azure.com/#create/Microsoft.CognitiveServicesTextTranslation",
	"volcengine":  "https://console.volcengine.com/iam/keymanage/",
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

// HotkeySvc checks whether a global shortcut is available.
type HotkeySvc struct{}

// Busy reports whether an accelerator is taken, by any app.
func (HotkeySvc) Busy(accel string) bool {
	return mygo.GlobalShortcut.IsRegistered(accel)
}
