//go:build darwin

package settings

import (
	"github.com/egoist/mygo/ui"
	"path/filepath"
	"sotto/internal/config"
	"sync"
	"testing"
	"time"
)

func TestSettingsCapturesCommandOnlyShortcutOnMac(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	v := NewView(TabHotkeys, current, Actions{SaveSettings: func(next config.Settings) error { mu.Lock(); current = next; mu.Unlock(); return nil }})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	tt.Frame()
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Super, ui.KeyK)
	waitForCondition(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return current.Hotkeys.Panel == "Cmd+K" }, "Command modifier was not saved")
	tt.Frame()
	if !tt.HasText("⌘K") {
		t.Fatalf("saved shortcut did not display Command symbol: %v", tt.Texts())
	}
}

func TestSettingsCapturesCommandOptionShortcuts(t *testing.T) {
	for _, tc := range []struct {
		label, action, want string
		key                 ui.Key
	}{
		{"翻译面板快捷键", "panel", "Option+Cmd+A", ui.KeyA},
		{"截图翻译快捷键", "ocr", "Option+Cmd+S", ui.KeyS},
		{"划词翻译快捷键", "selection", "Option+Cmd+D", ui.KeyD},
	} {
		t.Run(tc.action, func(t *testing.T) {
			var mu sync.Mutex
			current := testSettingsConfig()
			v := NewView(TabHotkeys, current, Actions{SaveSettings: func(next config.Settings) error { mu.Lock(); current = next; mu.Unlock(); return nil }})
			defer v.Dispose()
			tt := ui.NewTester(v.Render, 780, 600)
			if err := tt.Click(tc.label); err != nil {
				t.Fatal(err)
			}
			tt.Key(ui.Super|ui.Alt, tc.key)
			waitForCondition(t, time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return hotkeyValue(current.Hotkeys, tc.action) == tc.want }, "Command+Option shortcut was not saved")
			tt.Frame()
			if tt.HasText("按下组合键…") || !tt.HasText(displayHotkey(tc.want)) {
				t.Fatalf("recorder did not settle: %v", tt.Texts())
			}
		})
	}
}

func TestSettingsControlOptionSetSurvivesSaveAndReload(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	path := filepath.Join(t.TempDir(), "settings.json")
	v := NewView(TabHotkeys, current, Actions{
		GetSettings: func() config.Settings {
			mu.Lock()
			defer mu.Unlock()
			return current
		},
		SaveSettings: func(next config.Settings) error {
			next = config.Normalize(next)
			if err := config.Save(path, next); err != nil {
				return err
			}
			mu.Lock()
			current = next
			mu.Unlock()
			return nil
		},
	})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	for _, tc := range []struct {
		label string
		key   ui.Key
	}{
		{"翻译面板快捷键", ui.KeyA},
		{"截图翻译快捷键", ui.KeyS},
		{"划词翻译快捷键", ui.KeyD},
	} {
		if err := tt.Click(tc.label); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Ctrl|ui.Alt, tc.key)
		waitForCondition(t, time.Second, func() bool {
			v.mu.Lock()
			defer v.mu.Unlock()
			return v.pendingWrites == 0
		}, "shortcut save did not finish")
		tt.Frame()
	}
	want := config.HotkeySettings{Panel: "Ctrl+Option+A", Ocr: "Ctrl+Option+S", Selection: "Ctrl+Option+D"}
	mu.Lock()
	got := current.Hotkeys
	mu.Unlock()
	if got != want {
		t.Fatalf("complete Control+Option set was rewritten after third save: %+v", got)
	}
	v.Dispose()
	reloaded, err := config.Load(path)
	if err != nil || reloaded.Hotkeys != want {
		t.Fatalf("shortcuts changed on restart: %+v, err=%v", reloaded.Hotkeys, err)
	}
	reopened := NewView(TabHotkeys, reloaded, Actions{})
	defer reopened.Dispose()
	tt = ui.NewTester(reopened.Render, 780, 600)
	for _, accel := range []string{want.Panel, want.Ocr, want.Selection} {
		if !tt.HasText(displayHotkey(accel)) {
			t.Fatalf("reopened settings lost %q: %v", accel, tt.Texts())
		}
	}
}
func TestRecorderRecognizesCurrentOptionShortcut(t *testing.T) {
	cfg := testSettingsConfig()
	cfg.Hotkeys.Panel = "Option+A"
	v := NewView(TabHotkeys, cfg, Actions{})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Alt, ui.KeyA)
	v.mu.Lock()
	defer v.mu.Unlock()
	if conflict := v.hotkeyConflicts["panel"]; conflict != "" {
		t.Fatalf("current Option shortcut conflicted with its Alt alias: %s", conflict)
	}
	if v.hotkeyListening != "" {
		t.Fatal("recorder stayed active after entering the current shortcut")
	}
}

// The input callback must commit on key-down even if key-up arrives before
// another frame. Sampling KeysDown while rendering cannot satisfy this.
func TestRecorderUsesMacPhysicalPosition(t *testing.T) {
	v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	// The layout reports a different logical key; registration must use the
	// physical key Carbon will bind, just as for Option symbols and IMEs.
	v.captureHotkey("panel", ui.InputEvent{Kind: ui.InputKeyDown, Mods: ui.Alt, Key: ui.KeyZ, PhysicalKey: ui.KeyK})
	v.Dispose()
	<-v.taskDone
	v.mu.Lock()
	got := v.settings.Hotkeys.Panel
	v.mu.Unlock()
	if got != "Option+K" {
		t.Fatalf("captured %q instead of physical Option+K", got)
	}
}
