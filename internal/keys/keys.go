// Package keys reads and writes service credentials in the system
// keyring, so they never land in settings.json.
package keys

import (
	"errors"
	"fmt"
	"github.com/egoist/mygo"
	"slices"
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

// Names lists every keyring entry, so the settings page can enumerate them.
var Names = []string{
	DeeplKey, OpenAIKey, SiliconFlowKey, ZhipuKey, BaiduAppID, BaiduKey,
	TencentSecretID, TencentSecretKey, YoudaoAppKey, YoudaoSecret, AzureKey, AzureRegion,
	VolcAccessKey, VolcSecretKey,
}

// ErrUnavailable distinguishes a temporarily inaccessible keyring from an absent key.
var ErrUnavailable = errors.New("system credentials unavailable")

// Store never caches credentials across operations, so external changes recover
// on the next request without retaining secrets for the process lifetime.
type Store struct{}

func (Store) Set(name, value string) error {
	if value == "" {
		return mygo.Secrets.Delete(name)
	}
	return mygo.Secrets.Set(name, []byte(value))
}

// ForServices includes optional request credentials as well as required ones.
func ForServices(services ...string) []string {
	var names []string
	for _, service := range services {
		for _, name := range serviceKeys[service] {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	return names
}

var serviceKeys = map[string][]string{
	"deepl": {DeeplKey}, "openai": {OpenAIKey},
	"siliconflow": {SiliconFlowKey}, "zhipu": {ZhipuKey},
	"baidu": {BaiduAppID, BaiduKey}, "tencent": {TencentSecretID, TencentSecretKey},
	"youdao": {YoudaoAppKey, YoudaoSecret}, "azure": {AzureKey, AzureRegion},
	"volcengine": {VolcAccessKey, VolcSecretKey},
}

// Snapshot owns the values and read failures for a single operation. Missing
// entries are empty; failures remain observable rather than disabling services.
type Snapshot struct {
	values   map[string]string
	failures map[string]error
}

func (s Snapshot) Get(name string) string { return s.values[name] }
func (s Snapshot) Err(names ...string) error {
	var errs []error
	if len(names) == 0 {
		names = Names
	}
	for _, name := range names {
		if err := s.failures[name]; err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
func (Store) Snapshot(names []string) Snapshot { return snapshot(names, mygo.Secrets.Get) }
func snapshot(names []string, read func(string) ([]byte, error)) Snapshot {
	out := Snapshot{values: make(map[string]string), failures: make(map[string]error)}
	for _, name := range names {
		value, err := read(name)
		switch {
		case errors.Is(err, mygo.ErrSecretNotFound):
		case err != nil:
			out.failures[name] = fmt.Errorf("%w: read %s: %w", ErrUnavailable, name, err)
		default:
			out.values[name] = string(value)
		}
	}
	return out
}
