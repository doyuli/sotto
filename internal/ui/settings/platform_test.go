package settings

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
)

func TestSettingsPlatformCopyAndShortcutPresentation(t *testing.T) {
	label, _ := credentialProtectionCopy(runtime.GOOS)
	for _, dark := range []bool{false, true} {
		for _, width := range []int{660, 780} {
			t.Run(fmt.Sprintf("dark=%t/width=%d", dark, width), func(t *testing.T) {
				v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
				defer v.Dispose()
				tt := ui.NewTester(v.Render, width, 600)
				tt.SetDark(dark)
				tt.Frame()
				if !tt.HasText(label) {
					t.Fatalf("missing platform credential label: %v", tt.Texts())
				}
				for _, key := range []string{config.Default().Hotkeys.Panel, config.Default().Hotkeys.Ocr, config.Default().Hotkeys.Selection} {
					if !tt.HasText(displayHotkey(key)) {
						t.Fatalf("missing native shortcut %s", displayHotkey(key))
					}
				}
				if runtime.GOOS == "darwin" && tt.HasText("Windows 凭据保护") {
					t.Fatal("Windows credential label leaked into Mac settings")
				}
				if output := os.Getenv("SOTTO_SETTINGS_PREVIEW"); output != "" {
					if err := os.MkdirAll(output, 0755); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(output, fmt.Sprintf("settings-%d-dark-%t.png", width, dark)))
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if err := png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSettingsCapturesCommandOnlyShortcutOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Mac Command key capture")
	}
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
	if runtime.GOOS != "darwin" {
		t.Skip("Mac Command+Option capture")
	}
	for _, tc := range []struct {
		label, action, want string
		key                 ui.Key
	}{
		{"翻译面板快捷键", "panel", "Alt+Cmd+A", ui.KeyA},
		{"截图翻译快捷键", "ocr", "Alt+Cmd+S", ui.KeyS},
		{"划词翻译快捷键", "selection", "Alt+Cmd+D", ui.KeyD},
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
	if runtime.GOOS != "darwin" {
		t.Skip("Mac Control+Option shortcut persistence")
	}
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
	want := config.HotkeySettings{Panel: "Ctrl+Alt+A", Ocr: "Ctrl+Alt+S", Selection: "Ctrl+Alt+D"}
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
