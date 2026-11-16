package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/platform"
	"sotto/internal/translate"
	"sotto/internal/ui/settings"
)

type credentialStore interface {
	Snapshot([]string) keys.Snapshot
	Set(string, string) error
}

// ConfigSvc reads and writes the settings and credentials for the native settings UI.
type ConfigSvc struct{ app *application }

func (s ConfigSvc) Get() config.Settings { return s.app.settings() }

// Set normalizes, saves and applies the settings.
func (s ConfigSvc) Set(settings config.Settings) error {
	return s.app.updateSettings(settings)
}

// SaveService commits the changed fields against the latest settings. Credentials
// live in the keyring and are restored if a later write fails.
func (s ConfigSvc) SaveService(name string, changes settings.ServiceChanges) (settings.ServiceSaveResult, error) {
	a := s.app
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	previous := a.settings()
	next := previous
	if changes.BaseURL != nil {
		if name != "openai" {
			return settings.ServiceSaveResult{}, errors.New("该服务不支持修改 Base URL")
		}
		next.Services.OpenAI.BaseURL = strings.TrimSpace(*changes.BaseURL)
	}
	if changes.Model != nil {
		model := next.Services.Model(name)
		if model == nil {
			return settings.ServiceSaveResult{}, errors.New("该服务不支持修改模型")
		}
		*model = strings.TrimSpace(*changes.Model)
	}
	names := slices.Sorted(maps.Keys(changes.Credentials))
	allowed := keys.ForServices(name)
	for _, key := range names {
		if !slices.Contains(allowed, key) {
			return settings.ServiceSaveResult{}, errors.New("该服务不支持此凭证")
		}
		if changes.Credentials[key] == "" && slices.Contains(next.Services.Enabled, name) &&
			!slices.Contains(translate.WithoutCredential(next, key), name) {
			next.Services.Enabled = slices.DeleteFunc(slices.Clone(next.Services.Enabled), func(enabled string) bool {
				return enabled == name
			})
		}
	}

	var original keys.Snapshot
	if len(names) != 0 {
		original = a.keys.Snapshot(names)
		if err := original.Err(names...); err != nil {
			return settings.ServiceSaveResult{}, fmt.Errorf("读取原有凭证：%w", err)
		}
	}
	var written []string
	rollback := func(err error) error {
		for _, key := range slices.Backward(written) {
			if restoreErr := a.keys.Set(key, original.Get(key)); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("恢复原有凭证失败：%w", restoreErr))
			}
		}
		return err
	}
	for _, key := range names {
		value := changes.Credentials[key]
		if value != "" {
			value = strings.TrimSpace(value)
			if value == "" {
				return settings.ServiceSaveResult{}, rollback(errors.New("请输入凭证"))
			}
		}
		if value == original.Get(key) {
			continue
		}
		if err := a.keys.Set(key, value); err != nil {
			return settings.ServiceSaveResult{}, rollback(fmt.Errorf("写入凭证：%w", err))
		}
		written = append(written, key)
	}
	disabled := !slices.Equal(next.Services.Enabled, previous.Services.Enabled)
	if changes.BaseURL != nil || changes.Model != nil || disabled {
		if err := a.saveSettings(config.Normalize(next)); err != nil {
			return settings.ServiceSaveResult{}, rollback(err)
		}
	}
	return settings.ServiceSaveResult{Disabled: disabled}, nil
}

// translationCredentials keeps the saved options and credentials in step for
// keyed requests. Keyless requests can use the in-memory config they already have.
func (a *application) translationCredentials(ctx context.Context, services *config.ServiceSettings, retry string) (keys.Snapshot, error) {
	if len(translationCredentialNames(*services, retry)) == 0 {
		return nil, ctx.Err()
	}

	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	*services = a.settings().Services
	names := translationCredentialNames(*services, retry)
	if len(names) == 0 {
		return nil, nil
	}
	return a.keys.Snapshot(names), nil
}

func translationCredentialNames(services config.ServiceSettings, retry string) []string {
	if retry != "" {
		return keys.ForServices(retry)
	}
	return keys.ForServices(services.Enabled...)
}

// Credentials reports presence and enablement eligibility for one service,
// or for every service when name is empty.
func (s ConfigSvc) Credentials(name string) (settings.CredentialStatus, error) {
	a := s.app
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	names := keys.Names
	if name != "" {
		names = keys.ForServices(name)
	}
	credentials := a.keys.Snapshot(names)
	if err := credentials.Err(names...); err != nil {
		return settings.CredentialStatus{}, errors.New(translate.UserMessage(err))
	}
	configured := translate.Configured(a.settings(), credentials)
	if name != "" {
		configured = slices.DeleteFunc(configured, func(service string) bool { return service != name })
	}
	status := settings.CredentialStatus{Stored: []string{}, Configured: configured}
	for _, key := range names {
		if credentials.Get(key) != "" {
			status.Stored = append(status.Stored, key)
		}
	}
	return status, nil
}

// TestService returns a user-facing error, or an empty string on success.
func (s ConfigSvc) TestService(ctx context.Context, name string) string {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	a := s.app
	a.configWriteMu.Lock()
	if err := ctx.Err(); err != nil {
		a.configWriteMu.Unlock()
		return translate.UserMessage(err)
	}
	cfg := a.settings()
	credentials := a.keys.Snapshot(keys.ForServices(name))
	a.configWriteMu.Unlock()
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
	return platform.OpenInBrowser(page)
}

// DeeplUsage reports this month's DeepL character count and limit.
func (s ConfigSvc) DeeplUsage(ctx context.Context) (settings.DeeplUsage, error) {
	if err := ctx.Err(); err != nil {
		return settings.DeeplUsage{}, err
	}
	a := s.app
	a.configWriteMu.Lock()
	if err := ctx.Err(); err != nil {
		a.configWriteMu.Unlock()
		return settings.DeeplUsage{}, err
	}
	credentials := a.keys.Snapshot([]string{keys.DeeplKey})
	a.configWriteMu.Unlock()
	if err := credentials.Err(); err != nil {
		return settings.DeeplUsage{}, errors.New(translate.UserMessage(err))
	}
	used, limit, err := translate.DeepL{}.Usage(ctx, credentials.Get(keys.DeeplKey))
	return settings.DeeplUsage{Used: used, Limit: limit}, err
}
