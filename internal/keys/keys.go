// Package keys reads and writes service credentials in the system
// keyring, so they never land in settings.json.
package keys

import "github.com/egoist/mygo"

// The keyring entries, by service.
const (
	DeeplKey   = "deepl-key"
	OpenAIKey  = "openai-key"
	BaiduAppID = "baidu-appid"
	BaiduKey   = "baidu-key"

	TencentSecretID  = "tencent-secretid"
	TencentSecretKey = "tencent-secretkey"
	YoudaoAppKey     = "youdao-appkey"
	YoudaoSecret     = "youdao-secret"
	AzureKey         = "azure-key"
	AzureRegion      = "azure-region"
	VolcAccessKey    = "volcengine-access-key"
	VolcSecretKey    = "volcengine-secret-key"
)

// Names lists every keyring entry, so the settings page can enumerate them.
var Names = []string{
	DeeplKey, OpenAIKey, BaiduAppID, BaiduKey,
	TencentSecretID, TencentSecretKey, YoudaoAppKey, YoudaoSecret, AzureKey, AzureRegion,
	VolcAccessKey, VolcSecretKey,
}

// Store is the app's KeyStore: a thin wrapper over mygo.Secrets. A missing
// entry reads as an empty string rather than an error, which is what the
// translation services check for.
type Store struct{}

// Get returns the stored value, or "" when there is none.
func (Store) Get(name string) string {
	value, err := mygo.Secrets.Get(name)
	if err != nil {
		return ""
	}
	return string(value)
}

// Set stores the value; an empty value deletes the entry.
func (Store) Set(name, value string) error {
	if value == "" {
		return mygo.Secrets.Delete(name)
	}
	return mygo.Secrets.Set(name, []byte(value))
}

// Snapshot lets a settings operation evaluate the same set of credentials
// throughout, including checking the effect of removing a credential.
type Snapshot map[string]string

func (s Snapshot) Get(name string) string { return s[name] }

func (s Store) Snapshot() Snapshot {
	out := make(Snapshot)
	for _, name := range Names {
		if value := s.Get(name); value != "" {
			out[name] = value
		}
	}
	return out
}
