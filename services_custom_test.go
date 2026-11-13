package main

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/keys"
)

// The application writer lock serializes access to this fake, just as to the keyring.
type memoryCredentials struct {
	values    keys.Snapshot
	readErr   error
	beforeSet func(string, string) error
	afterSet  func()
}

func (m *memoryCredentials) Get(name string) string           { return m.values[name] }
func (m *memoryCredentials) Read(name string) (string, error) { return m.values[name], m.readErr }
func (m *memoryCredentials) Set(name, value string) error {
	if m.beforeSet != nil {
		if err := m.beforeSet(name, value); err != nil {
			return err
		}
	}
	if value == "" {
		delete(m.values, name)
	} else {
		m.values[name] = value
	}
	if m.afterSet != nil {
		m.afterSet()
	}
	return nil
}
func (m *memoryCredentials) Snapshot(_ ...string) keys.Snapshot { return maps.Clone(m.values) }

func customServiceTestApp(t *testing.T, exists bool) (*application, *memoryCredentials, config.CustomService) {
	t.Helper()
	service := config.CustomService{ID: "custom:1", Name: "Local", BaseURL: "http://old.example/v1", Model: "old"}
	cfg := config.Default()
	store := &memoryCredentials{values: make(keys.Snapshot)}
	if exists {
		cfg.Services.Custom = []config.CustomService{service}
		cfg.Services.Enabled = []string{service.ID}
		cfg.Services.Order = append(cfg.Services.Order, service.ID)
		store.values[keys.CustomAPIKey(service.ID)] = "old-secret"
	}
	a := &application{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "settings.json"), keys: store, bubbles: newBubbleManager(nil)}
	if err := config.Save(a.cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	return a, store, service
}

func TestCustomSettingsCredentialRollback(t *testing.T) {
	for _, operation := range []string{"create", "replace", "delete"} {
		for _, failure := range []string{"none", "settings", "read", "write"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				a, store, service := customServiceTestApp(t, operation != "create")
				before := cloneSettings(a.settings())
				next := cloneSettings(before)
				newSecret := "new-secret"
				var credential *string
				if operation == "delete" {
					next.Services.Custom = nil
					next.Services.Enabled = nil
				} else {
					service.BaseURL = "http://new.example/v1"
					next.Services.Custom = []config.CustomService{service}
					credential = &newSecret
				}
				if failure == "settings" {
					a.cfgPath = filepath.Join(t.TempDir(), "missing", "settings.json")
				}
				keyErr := errors.New("keyring unavailable")
				if failure == "read" {
					store.readErr = keyErr
				}
				store.beforeSet = func(_, _ string) error {
					if failure == "write" {
						return keyErr
					}
					return nil
				}
				err := (ConfigSvc{a}).SetCustom(next, service.ID, credential)
				cfg, credentials := a.settingsSnapshot()
				keyName := keys.CustomAPIKey(service.ID)
				if failure == "none" {
					if err != nil {
						t.Fatal(err)
					}
					if operation == "delete" {
						if len(cfg.Services.Custom) != 0 || credentials.Get(keyName) != "" {
							t.Fatal("delete did not commit both stores")
						}
					} else if cfg.Services.Custom[0].BaseURL != service.BaseURL || credentials.Get(keyName) != newSecret {
						t.Fatal("save did not commit matching endpoint and key")
					}
					return
				}
				if err == nil {
					t.Fatal("failed mutation reported success")
				}
				if (failure == "read" || failure == "write") && !errors.Is(err, keyErr) {
					t.Fatalf("lost keyring error: %v", err)
				}
				if !slices.Equal(cfg.Services.Custom, before.Services.Custom) || !slices.Equal(cfg.Services.Enabled, before.Services.Enabled) {
					t.Fatal("failed mutation changed active configuration")
				}
				wantSecret := "old-secret"
				if operation == "create" {
					wantSecret = ""
				}
				if credentials.Get(keyName) != wantSecret {
					t.Fatal("failed mutation exposed the wrong credential")
				}
				if store.Get(keyName) != wantSecret {
					t.Fatal("failed mutation did not restore the keyring")
				}
			})
		}
	}
}

func TestCustomRollbackFailurePausesUntilCredentialIsReconfigured(t *testing.T) {
	for _, recovery := range []string{"save", "clear"} {
		t.Run(recovery, func(t *testing.T) {
			a, store, service := customServiceTestApp(t, true)
			path := a.cfgPath
			other := config.CustomService{ID: "custom:2", Name: "Other", BaseURL: "http://other.example/v1", Model: "m"}
			a.cfg.Services.Custom = append(a.cfg.Services.Custom, other)
			a.cfg.Services.Enabled = append(a.cfg.Services.Enabled, other.ID)
			next := cloneSettings(a.settings())
			next.Services.Custom[0].BaseURL = "http://new.example/v1"
			newSecret := "new-secret"
			if recovery == "clear" {
				next.Services.Custom = []config.CustomService{other}
				next.Services.Enabled = []string{other.ID}
			}
			a.cfgPath = filepath.Join(t.TempDir(), "missing", "settings.json")
			writes := 0
			store.beforeSet = func(_, _ string) error {
				writes++
				if writes == 2 {
					return errors.New("keyring unavailable")
				}
				return nil
			}
			svc := ConfigSvc{a}
			if err := svc.SetCustom(next, service.ID, &newSecret); err == nil || !strings.Contains(err.Error(), "服务已暂停") {
				t.Fatalf("rollback failure did not report the paused service: %v", err)
			}
			cfg, _ := a.settingsSnapshot()
			if _, exists := cfg.Services.FindCustom(service.ID); exists {
				t.Fatal("unsafe service is still available to translation")
			}
			if _, exists := cfg.Services.FindCustom(other.ID); !exists {
				t.Fatal("rollback failure paused an unrelated service")
			}
			if message := svc.TestService(context.Background(), service.ID); !strings.Contains(message, "请重新配置") {
				t.Fatalf("paused probe message = %q", message)
			}
			if slices.Contains(svc.Credentials().Configured, service.ID) {
				t.Fatal("paused service is still reported as configured")
			}
			repair := next
			if recovery == "clear" {
				repair = cloneSettings(a.settings())
			}
			if err := svc.SetCustom(repair, service.ID, nil); err == nil || !strings.Contains(err.Error(), "服务已暂停") {
				t.Fatal("metadata-only save resumed a service with an unknown key")
			}
			store.beforeSet = nil
			if err := svc.SetCustom(repair, service.ID, &newSecret); err == nil {
				t.Fatal("retry bypassed the settings write failure")
			}
			if !a.blockedCredentials[keys.CustomAPIKey(service.ID)] {
				t.Fatal("a failed retry resumed the service")
			}

			v := newSettingsView(tabServices, a.settings(), settingsActions{
				getSettings: svc.Get,
				setSecret:   svc.SetSecret,
				credentials: func() (CredentialStatus, error) { return svc.Credentials(), nil },
			})
			defer v.dispose()
			waitSettingsIdle(t, v)
			v.mu.Lock()
			v.expanded[service.ID] = true
			v.mu.Unlock()
			tt := ui.NewTester(v.render, 780, 900)
			if !tt.HasText("已暂停") || !tt.HasText("服务已暂停，请重新填写或清除 API Key") {
				t.Fatal("settings did not explain how to resume the service")
			}

			a.cfgPath = path
			if recovery == "save" {
				if err := svc.SetCustom(next, service.ID, &newSecret); err != nil {
					t.Fatal(err)
				}
			} else {
				settingsScrollClick(t, tt, "清除API Key", 780, 900)
				waitSettingsIdle(t, v)
				tt.Frame()
				if tt.HasText("已暂停") {
					t.Fatal("clearing the absent key did not resume the local service")
				}
			}
			cfg, credentials := a.settingsSnapshot()
			if _, exists := cfg.Services.FindCustom(service.ID); !exists || len(a.blockedCredentials) != 0 {
				t.Fatal("credential reconfiguration did not resume the service")
			}
			wantKey := newSecret
			if recovery == "clear" {
				wantKey = ""
			}
			if credentials.Get(keys.CustomAPIKey(service.ID)) != wantKey {
				t.Fatal("resumed service has the wrong credential")
			}
		})
	}
}

func TestCustomSettingsSnapshotWaitsForCommit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			a, store, service := customServiceTestApp(t, true)
			if fail {
				a.cfgPath = filepath.Join(t.TempDir(), "missing", "settings.json")
			}
			started, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			first := true
			store.afterSet = func() {
				if first {
					first = false
					close(started)
					<-release
				}
			}
			next := cloneSettings(a.settings())
			next.Services.Custom[0].BaseURL = "http://new.example/v1"
			newSecret := "new-secret"
			written := make(chan error, 1)
			go func() { written <- (ConfigSvc{a}).SetCustom(next, service.ID, &newSecret) }()
			<-started
			type pair struct {
				cfg         config.Settings
				credentials keys.Snapshot
			}
			read, reading := make(chan pair, 1), make(chan struct{})
			go func() {
				close(reading)
				cfg, credentials := a.settingsSnapshot()
				read <- pair{cfg, credentials}
			}()
			<-reading
			select {
			case <-read:
				t.Fatal("snapshot observed a partial credential update")
			case <-time.After(25 * time.Millisecond):
			}
			unblock()
			if err := <-written; (err != nil) != fail {
				t.Fatalf("commit error = %v", err)
			}
			got := <-read
			wantURL, wantKey := next.Services.Custom[0].BaseURL, newSecret
			if fail {
				wantURL, wantKey = service.BaseURL, "old-secret"
			}
			if got.cfg.Services.Custom[0].BaseURL != wantURL || got.credentials.Get(keys.CustomAPIKey(service.ID)) != wantKey {
				t.Fatal("snapshot mixed settings and credentials from different updates")
			}
		})
	}
}

func TestOrdinarySettingsDoNotPublishStaleCustomDefinitions(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "after commit", true: "after failure"}[fail], func(t *testing.T) {
			a, _, service := customServiceTestApp(t, true)
			path := a.cfgPath
			stale := cloneSettings(a.settings())
			stale.Services.Custom[0].BaseURL = "http://new.example/v1"
			if fail {
				a.cfgPath = filepath.Join(t.TempDir(), "missing", "settings.json")
			}
			key := "new-secret"
			if err := (ConfigSvc{a}).SetCustom(stale, service.ID, &key); (err != nil) != fail {
				t.Fatalf("commit error = %v", err)
			}
			a.cfgPath = path
			// The next ordinary UI snapshot may contain either side of the custom edit.
			if !fail {
				stale.Services.Custom[0] = service
			}
			stale.TargetLanguage = "ja"
			if err := (ConfigSvc{a}).Set(stale); err != nil {
				t.Fatal(err)
			}
			cfg, credentials := a.settingsSnapshot()
			wantURL, wantKey := "http://new.example/v1", key
			if fail {
				wantURL, wantKey = service.BaseURL, "old-secret"
			}
			if cfg.TargetLanguage != "ja" || cfg.Services.Custom[0].BaseURL != wantURL || credentials.Get(keys.CustomAPIKey(service.ID)) != wantKey {
				t.Fatal("ordinary snapshot republished a mismatched custom definition")
			}
		})
	}
}
