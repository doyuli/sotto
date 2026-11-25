package app

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/hotkey"
	"sotto/internal/ui/bubble"
	"sotto/internal/ui/settings"
	"sotto/resources"
)

type failingShortcutBackend struct {
	hotkey.Backend
	shortcut string
	err      error
}

func (b failingShortcutBackend) Register(shortcut string, _ func()) error {
	if shortcut == b.shortcut {
		return b.err
	}
	return nil
}

func TestHotkeyRegistrationFailureShowsOnlyUserMessage(t *testing.T) {
	// MyGo's error used to appear after the Chinese hint in settings and startup notifications.
	cfg := config.Default()
	cfg.Hotkeys.Panel = "Ctrl+K"
	values := hotkeyValues(canonicalHotkeys(cfg.Hotkeys))
	for _, tc := range []struct{ action, label string }{
		{"panel", "翻译面板"},
		{"minimalTranslation", "简约翻译模式"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			accel := values[tc.action]
			a := &application{cfg: cfg, shortcutBackend: failingShortcutBackend{
				shortcut: accel,
				err:      fmt.Errorf("%w: mygo: cannot register %q: hotkey is already registered", hotkey.ErrSystemRegistration, accel),
			}}
			const message = "系统无法注册该组合键，可能已被占用，请更换组合键"
			if err := a.applyHotkeys(cfg.Hotkeys); err == nil || err.Error() != tc.label+"快捷键 "+accel+"："+message {
				t.Fatalf("startup notification = %v", err)
			}
			if got := a.shortcutErrors()[tc.action]; got != message {
				t.Fatalf("inactive shortcut message = %q", got)
			}
			if err := a.updateHotkey(tc.action, accel); err == nil || err.Error() != message {
				t.Fatalf("retry message = %v", err)
			}
		})
	}
}

func TestTrayAdvertisesOnlyRegisteredSelectionShortcut(t *testing.T) {
	// A failed registration should show its status without navigating away
	// from an open settings tab or advertising the dead shortcut.
	cfg := config.Default()
	cfg.Hotkeys.Selection = "Ctrl+Shift+D"
	key := hotkeyValues(canonicalHotkeys(cfg.Hotkeys))["selection"]
	a := &application{cfg: cfg, shortcutBackend: failingShortcutBackend{
		shortcut: key, err: hotkey.ErrSystemRegistration,
	}}
	if err := a.applyHotkeys(cfg.Hotkeys); err == nil {
		t.Fatal("expected selection registration failure")
	}
	item := mygo.NewMenu(a.trayTranslationItems()).ItemByID("selection")
	if item == nil || item.Label != "划词翻译（快捷键未生效）" || item.Accelerator != "" || item.IsEnabled() || item.Click != nil {
		t.Fatalf("unregistered shortcut should be a status hint: %+v", item)
	}
	a.shortcutBackend = failingShortcutBackend{}
	if err := a.applyHotkeys(cfg.Hotkeys); err != nil {
		t.Fatal(err)
	}
	item = mygo.NewMenu(a.trayTranslationItems()).ItemByID("selection")
	if item == nil || item.Accelerator != key || item.IsEnabled() || item.Click != nil {
		t.Fatalf("registered shortcut should be a hint: %+v", item)
	}
}

func TestMinimalTranslationToggleSurvivesQueuedSettingsSave(t *testing.T) {
	// A general settings snapshot taken before the shortcut must not undo it.
	cfg := config.Default()
	accel := hotkeyValues(canonicalHotkeys(cfg.Hotkeys))["minimalTranslation"]
	a := &application{
		cfg:     cfg,
		cfgPath: filepath.Join(t.TempDir(), "settings.json"),
		bubbles: bubble.NewManager(nil, bubble.Actions{}),
		hotkeys: map[string]string{"minimalTranslation": accel},
	}
	stale := cfg
	stale.TargetLanguage = "ja"
	// A stale form must not replace any independently registered accelerator.
	stale.Hotkeys.Panel = "Ctrl+Shift+P"
	if item := mygo.NewMenu(a.trayTranslationItems()).ItemByID("minimalTranslation"); item == nil || item.IsChecked() {
		t.Fatal("tray should offer the unchecked mode before the toggle")
	}
	synctest.Test(t, func(t *testing.T) {
		a.onShortcut(accel)()
		synctest.Wait()
	})
	if err := a.updateSettings(stale); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(a.cfgPath)
	if err != nil || !saved.MinimalTranslation || saved.TargetLanguage != "ja" || saved.Hotkeys != cfg.Hotkeys || !a.settings().MinimalTranslation {
		t.Fatalf("saved mode=%v language=%s err=%v", saved.MinimalTranslation, saved.TargetLanguage, err)
	}
	item := mygo.NewMenu(a.trayTranslationItems()).ItemByID("minimalTranslation")
	if item == nil || !item.IsChecked() || !bytes.Equal(a.trayOptions().Icon, resources.TrayOutline) {
		t.Fatal("tray did not show the committed mode")
	}
	synctest.Test(t, func(t *testing.T) {
		item.Click(item, nil)
		synctest.Wait()
	})
	item = mygo.NewMenu(a.trayTranslationItems()).ItemByID("minimalTranslation")
	if item == nil || item.IsChecked() || !bytes.Equal(a.trayOptions().Icon, resources.TrayFilled) {
		t.Fatal("tray did not return to bubble mode")
	}
}

func TestSettingsViewReceivesModeToggleBeforeWindowCreation(t *testing.T) {
	// A toggle during native window creation used to miss the new settings view.
	a := &application{
		cfg:     config.Default(),
		cfgPath: filepath.Join(t.TempDir(), "settings.json"),
		keys:    &serviceTestKeyring{},
		bubbles: bubble.NewManager(nil, bubble.Actions{}),
	}
	view := a.createSettingsView(settings.TabGeneral)
	defer view.Dispose()

	if enabled, err := a.updateMinimalTranslation(func(current bool) bool { return !current }); err != nil || !enabled {
		t.Fatalf("toggle enabled=%v err=%v", enabled, err)
	}
	tt := ui.NewTester(view.Render, 780, 600)
	sw, found := tt.AccessByRole(ui.RoleSwitch, "切换简约翻译模式")
	if !found || !sw.Checked {
		t.Fatalf("first frame did not show the enabled mode: found=%v checked=%v", found, sw.Checked)
	}
}

func TestMinimalTranslationToggleKeepsModeOnSaveFailure(t *testing.T) {
	// A failed write must leave the active mode at its previous value.
	cfg := config.Default()
	cfg.MinimalTranslation = true
	a := &application{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "missing", "settings.json")}
	enabled, err := a.updateMinimalTranslation(func(current bool) bool { return !current })
	if err == nil || !enabled || !a.settings().MinimalTranslation {
		t.Fatalf("toggle enabled=%v err=%v, active mode=%v", enabled, err, a.settings().MinimalTranslation)
	}
}

func TestHotkeyCommitKeepsOldBindingOnFailure(t *testing.T) {
	for _, diskFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "registration", true: "persistence"}[diskFailure], func(t *testing.T) {
			old := map[string]string{"panel": "Ctrl+A", "ocr": "Ctrl+S"}
			desired := map[string]string{"panel": "Ctrl+B", "ocr": "Ctrl+T"}
			held := map[string]bool{"Ctrl+A": true, "Ctrl+S": true}
			published := false
			failure := errors.New("failed")
			err := commitHotkeys(old, desired, func(s string) error {
				if !held["Ctrl+A"] || !held["Ctrl+S"] {
					t.Fatal("released working shortcuts before commit")
				}
				if !diskFailure && s == "Ctrl+T" {
					return failure
				}
				held[s] = true
				return nil
			}, func(s string) { delete(held, s) }, func() error {
				if diskFailure {
					return failure
				}
				t.Fatal("persisted a failed registration")
				return nil
			}, func(map[string]string) { published = true })
			if !errors.Is(err, failure) || published || len(held) != 2 || !held["Ctrl+A"] || !held["Ctrl+S"] {
				t.Fatalf("error=%v held=%v published=%v", err, held, published)
			}
		})
	}
}

func TestHotkeyCommitSwapsWithoutReregistering(t *testing.T) {
	old := map[string]string{"panel": "Ctrl+A", "ocr": "Ctrl+S"}
	desired := map[string]string{"panel": "Ctrl+S", "ocr": "Ctrl+A"}
	var active map[string]string
	persisted := false
	err := commitHotkeys(old, desired, func(string) error { t.Fatal("re-registered an existing binding"); return nil }, func(string) { t.Fatal("released a reused binding") }, func() error { persisted = true; return nil }, func(next map[string]string) {
		if !persisted {
			t.Fatal("published before persistence")
		}
		active = next
	})
	if err != nil || !maps.Equal(active, desired) {
		t.Fatalf("active=%v err=%v", active, err)
	}
}

func TestHotkeyCommitReleasesOnlyObsoleteBindingsAfterPublish(t *testing.T) {
	old := map[string]string{"panel": "Ctrl+A", "ocr": "Ctrl+S"}
	desired := map[string]string{"panel": "Ctrl+B", "ocr": "Ctrl+S"}
	published := false
	var registered, removed string
	err := commitHotkeys(old, desired, func(s string) error { registered = s; return nil }, func(s string) {
		if !published {
			t.Fatal("released before publish")
		}
		removed = s
	}, func() error { return nil }, func(map[string]string) { published = true })
	if err != nil || registered != "Ctrl+B" || removed != "Ctrl+A" {
		t.Fatalf("registered=%s removed=%s err=%v", registered, removed, err)
	}
}
