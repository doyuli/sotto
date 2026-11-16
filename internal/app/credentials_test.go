package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/ui/bubble"
	"sotto/internal/ui/settings"
)

func TestDeeplUsageRejectsCanceledRequestBeforeReadingCredentials(t *testing.T) {
	// A query canceled while queued must not start keyring or network work.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &application{keys: &unusedKeyring{t: t}}
	if _, err := (ConfigSvc{a}).DeeplUsage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled usage: %v", err)
	}
}

func TestKeylessTranslationDoesNotWaitForSettingsCredentialRead(t *testing.T) {
	// Opening Settings may hold the configuration lock while the keyring waits for authorization.
	a := &application{keys: &unusedKeyring{t: t}}
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	for _, retry := range []string{"", "google"} {
		services := config.Default().Services
		if retry != "" {
			services.Enabled = []string{"openai", retry}
		}
		done := make(chan error, 1)
		go func() {
			_, err := a.translationCredentials(t.Context(), &services, retry)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("a keyless request waited for the configuration lock")
		}
	}
}

func TestCanceledTranslationSkipsKeyringAfterAcquiringLock(t *testing.T) {
	// A request canceled behind a settings read must stop before any keyring access.
	a := &application{keys: &unusedKeyring{t: t}}
	services := config.Default().Services
	services.Enabled = []string{"openai"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	a.configWriteMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := a.translationCredentials(ctx, &services, "")
		done <- err
	}()
	<-started
	cancel()
	a.configWriteMu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled translation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled translation did not finish")
	}
}

type unusedKeyring struct {
	t *testing.T
}

func (k *unusedKeyring) Snapshot([]string) keys.Snapshot {
	k.t.Fatal("settings mutation read the keyring")
	return nil
}
func (k *unusedKeyring) Set(string, string) error {
	k.t.Fatal("unrelated settings mutation wrote the keyring")
	return nil
}

func TestSettingsPreserveServicesWithoutReadingKeyring(t *testing.T) {
	// Saving an unrelated setting must not erase enabled services on keyring failure.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"openai", "deepl", "azure"}
	keyring := &unusedKeyring{t: t}
	a := &application{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "settings.json"), keys: keyring, bubbles: bubble.NewManager(nil, bubble.Actions{})}
	changed := cfg
	changed.TargetLanguage = "ja"
	if err := a.updateSettings(changed); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(a.cfgPath)
	if err != nil || !slices.Equal(saved.Services.Enabled, cfg.Services.Enabled) {
		t.Fatalf("saved=%v err=%v", saved.Services.Enabled, err)
	}
}

type serviceTestKeyring struct {
	values   map[string]string
	fail     string
	failRead string
	writes   []string
}

type testCredentialSnapshot struct {
	values map[string]string
	err    error
}

func (s testCredentialSnapshot) Get(name string) string { return s.values[name] }
func (s testCredentialSnapshot) Err(...string) error    { return s.err }

func (k *serviceTestKeyring) Snapshot(names []string) keys.Snapshot {
	out := testCredentialSnapshot{values: make(map[string]string, len(names))}
	for _, name := range names {
		if name == k.failRead {
			out.err = keys.ErrUnavailable
			continue
		}
		out.values[name] = k.values[name]
	}
	return out
}

func (k *serviceTestKeyring) Set(name, value string) error {
	if name == k.fail {
		return errors.New("keyring locked")
	}
	k.writes = append(k.writes, value)
	if value == "" {
		delete(k.values, name)
	} else {
		k.values[name] = value
	}
	return nil
}

func TestServiceCredentialsIgnoreUnrelatedKeyringFailure(t *testing.T) {
	// An unrelated unreadable credential must not block the edited service's status.
	keyring := &serviceTestKeyring{
		values:   map[string]string{keys.OpenAIKey: "key"},
		failRead: keys.DeeplKey,
	}
	svc := ConfigSvc{&application{cfg: config.Default(), keys: keyring}}
	if _, err := svc.Credentials(""); err == nil {
		t.Fatal("the full read did not report the unreadable credential")
	}
	status, err := svc.Credentials("openai")
	if err != nil || !slices.Equal(status.Stored, []string{keys.OpenAIKey}) ||
		!slices.Equal(status.Configured, []string{"openai"}) {
		t.Fatalf("scoped read did not return the edited service: %+v, %v", status, err)
	}
}

func TestSaveServiceRollsBackWhenSettingsWriteFails(t *testing.T) {
	// A succeeded keyring write cannot leave the new key paired with the old endpoint.
	cfg := config.Default()
	keyring := &serviceTestKeyring{values: map[string]string{keys.OpenAIKey: "old-key"}}
	root := t.TempDir()
	a := &application{cfg: cfg, cfgPath: filepath.Join(root, "missing", "settings.json"), keys: keyring, bubbles: bubble.NewManager(nil, bubble.Actions{})}
	baseURL := "https://proxy.example/v1"
	changes := settings.ServiceChanges{
		BaseURL:     &baseURL,
		Credentials: map[string]string{keys.OpenAIKey: "new-key"},
	}
	svc := ConfigSvc{a}
	if _, err := svc.SaveService("openai", changes); err == nil {
		t.Fatal("save succeeded without a settings directory")
	}
	if keyring.values[keys.OpenAIKey] != "old-key" || !slices.Equal(keyring.writes, []string{"new-key", "old-key"}) ||
		a.settings().Services.OpenAI != cfg.Services.OpenAI {
		t.Fatal("failed save did not restore the previous key and configuration")
	}
	a.cfgPath = filepath.Join(root, "settings.json")
	if _, err := svc.SaveService("openai", changes); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(a.cfgPath)
	if err != nil || saved.Services.OpenAI.BaseURL != baseURL || keyring.values[keys.OpenAIKey] != "new-key" {
		t.Fatalf("retry did not commit both fields: %v", err)
	}
}

func TestSaveServiceRollsBackMultipleCredentials(t *testing.T) {
	// A failed second credential write cannot leave half of a credential pair configured.
	keyring := &serviceTestKeyring{values: map[string]string{}, fail: keys.BaiduKey}
	a := &application{cfg: config.Default(), keys: keyring}
	changes := settings.ServiceChanges{
		Credentials: map[string]string{keys.BaiduAppID: "id", keys.BaiduKey: "key"},
	}
	if _, err := (ConfigSvc{a}).SaveService("baidu", changes); err == nil || len(keyring.values) != 0 {
		t.Fatal("failed credential pair was not rolled back")
	}
}

func TestSaveServiceRemovingCredentialUpdatesEnablement(t *testing.T) {
	// Only removing a required credential disables a service.
	cfg := config.Default()
	cfg.Services.Enabled = []string{"openai", "deepl", "azure"}
	keyring := &serviceTestKeyring{values: map[string]string{
		keys.AzureKey: "azure-key", keys.AzureRegion: "region", keys.OpenAIKey: "openai-key",
	}}
	a := &application{
		cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "settings.json"),
		keys: keyring, bubbles: bubble.NewManager(nil, bubble.Actions{}),
	}
	svc := ConfigSvc{a}
	result, err := svc.SaveService("azure", settings.ServiceChanges{Credentials: map[string]string{keys.AzureRegion: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Disabled || !slices.Equal(a.settings().Services.Enabled, cfg.Services.Enabled) ||
		keyring.values[keys.AzureRegion] != "" || keyring.values[keys.AzureKey] != "azure-key" {
		t.Fatal("optional region removal did not preserve the key and enablement")
	}
	result, err = svc.SaveService("openai", settings.ServiceChanges{Credentials: map[string]string{keys.OpenAIKey: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Disabled || keyring.values[keys.OpenAIKey] != "" {
		t.Fatal("required credential removal did not delete the key and report disablement")
	}
	saved, err := config.Load(a.cfgPath)
	if err != nil || !slices.Equal(saved.Services.Enabled, []string{"deepl", "azure"}) {
		t.Fatalf("removal changed unrelated services: %v, %v", saved.Services.Enabled, err)
	}
}
