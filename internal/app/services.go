package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/platform"
	"sotto/internal/translate"
	"sotto/internal/ui/settings"
)

// ConfigSvc reads and writes the settings and credentials for the native settings UI.
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

// Credentials reports which credentials are stored and which services can be enabled.
func (s ConfigSvc) Credentials() settings.CredentialStatus {
	credentials := s.app.keys.Snapshot()
	status := settings.CredentialStatus{Stored: []string{}, Configured: translate.Configured(s.app.settings(), credentials)}
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
	return platform.OpenInBrowser(page)
}

// DeeplUsage reports this month's DeepL character count and limit.
func (ConfigSvc) DeeplUsage() (settings.DeeplUsage, error) {
	used, limit, err := translate.DeepL{}.Usage(context.Background(), keys.Store{}.Get(keys.DeeplKey))
	return settings.DeeplUsage{Used: used, Limit: limit}, err
}
