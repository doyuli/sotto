// Package keys reads and writes service credentials in the system
// keyring, so they never land in settings.json.
package keys

import (
	"errors"
	"strings"

	"github.com/egoist/mygo"
)

// The keyring entries, by service.
const (
	DeeplKey       = "deepl-key"
	OpenAIKey      = "openai-key"
	SiliconFlowKey = "siliconflow-key"
	ZhipuKey       = "zhipu-key"
	BaiduAppID     = "baidu-appid"
	BaiduKey       = "baidu-key"

	TencentSecretID  = "tencent-secretid"
	TencentSecretKey = "tencent-secretkey"
	YoudaoAppKey     = "youdao-appkey"
	YoudaoSecret     = "youdao-secret"
	AzureKey         = "azure-key"
	AzureRegion      = "azure-region"
	VolcAccessKey    = "volcengine-access-key"
	VolcSecretKey    = "volcengine-secret-key"
)

// Names lists the built-in keyring entries.
var Names = []string{
	DeeplKey, OpenAIKey, SiliconFlowKey, ZhipuKey, BaiduAppID, BaiduKey,
	TencentSecretID, TencentSecretKey, YoudaoAppKey, YoudaoSecret, AzureKey, AzureRegion,
	VolcAccessKey, VolcSecretKey,
}

// Store is the app's KeyStore: a thin wrapper over mygo.Secrets. A missing
// entry reads as an empty string rather than an error, which is what the
// translation services check for.
type Store struct{}

// Get returns the stored value, or "" when there is none.
func (s Store) Get(name string) string {
	value, err := s.Read(name)
	if err != nil {
		return ""
	}
	return value
}

// Read distinguishes a missing credential from a keyring failure before an update.
func (Store) Read(name string) (string, error) {
	value, err := mygo.Secrets.Get(name)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return "", nil
	}
	return string(value), err
}

// Set stores the value; an empty value deletes the entry.
func (Store) Set(name, value string) error {
	if value == "" {
		return mygo.Secrets.Delete(name)
	}
	return mygo.Secrets.Set(name, []byte(value))
}

// CustomAPIKey returns the keyring entry name for a custom service ID.
func CustomAPIKey(serviceID string) string {
	suffix := strings.TrimPrefix(serviceID, "custom:")
	if suffix == "" || suffix == serviceID {
		return ""
	}
	return "custom-" + suffix + "-key"
}

// IsCustomKey reports whether name is a keyring entry for a custom service.
func IsCustomKey(name string) bool {
	return strings.HasPrefix(name, "custom-") && strings.HasSuffix(name, "-key") && len(name) > len("custom--key")
}

// Snapshot lets a settings operation evaluate the same set of credentials
// throughout, including checking the effect of removing a credential.
type Snapshot map[string]string

func (s Snapshot) Get(name string) string { return s[name] }

// Snapshot reads the built-in keyring entries plus any extra custom keys.
func (s Store) Snapshot(extra ...string) Snapshot {
	out := make(Snapshot)
	for _, names := range [][]string{Names, extra} {
		for _, name := range names {
			if name == "" {
				continue
			}
			if value := s.Get(name); value != "" {
				out[name] = value
			}
		}
	}
	return out
}
