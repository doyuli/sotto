package settings

import (
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMacShortcutDisplayAndCommandCapture(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{"Alt+A", "⌥A"}, {"Option+A", "⌥A"}, {"Option+Shift+S", "⌥⇧S"}, {"Alt+Shift+S", "⌥⇧S"}, {"Cmd+K", "⌘K"}, {"Ctrl+Alt+Shift+Cmd+K", "⌃⌥⇧⌘K"}, {"CmdOrCtrl++", "⌘+"},
	} {
		if got := hotkeyDisplayForPlatform(tc.stored, "darwin"); got != tc.want {
			t.Fatalf("%q displayed as %q, want %q", tc.stored, got, tc.want)
		}
		if got := hotkeyDisplayForPlatform(tc.stored, "windows"); got != tc.stored {
			t.Fatalf("Windows display changed: %q", got)
		}
	}
	for _, tc := range []struct {
		mods      ui.Modifiers
		key, want string
	}{
		{ui.Super, "K", "Cmd+K"}, {ui.Super | ui.Shift, "K", "Shift+Cmd+K"}, {ui.Alt, "S", "Alt+S"}, {ui.Ctrl | ui.Alt | ui.Super, "K", "Ctrl+Alt+Cmd+K"}, {0, "A", ""}, {ui.Super, "", ""},
	} {
		if got := capturedHotkey(tc.mods, tc.key, "darwin"); got != tc.want {
			t.Fatalf("capture=%q want=%q", got, tc.want)
		}
	}
}

func TestRecorderRecognizesCurrentOptionShortcut(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Mac Option modifier alias")
	}
	cfg := testSettingsConfig()
	cfg.Hotkeys.Panel = "Option+A"
	v := NewView(TabHotkeys, cfg, Actions{HotkeyBusy: func(string) bool { return true }})
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
func TestRecorderCommitsBeforeNextFrame(t *testing.T) {
	v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("翻译面板快捷键") {
		t.Fatal("recorder did not receive keyboard focus")
	}
	for _, event := range []ui.InputEvent{
		{Kind: ui.InputKeyDown, Mods: ui.Ctrl | ui.Shift, Key: ui.KeyK},
		{Kind: ui.InputKeyUp, Mods: ui.Ctrl | ui.Shift, Key: ui.KeyK},
	} {
		v.captureHotkey("panel", event)
	}
	v.mu.Lock()
	saved, listening := v.settings.Hotkeys.Panel, v.hotkeyListening
	v.mu.Unlock()
	if saved != "Ctrl+Shift+K" || listening != "" {
		t.Fatalf("shortcut=%q listening=%q", saved, listening)
	}
	tt.Frame()
	if tt.HasText("按下组合键…") {
		t.Fatal("recorder stayed active after key release")
	}
}

func TestRecorderTakesSpaceAndWaitsForModifier(t *testing.T) {
	v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("截图翻译快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyK)
	if !tt.HasText("按下组合键…") {
		t.Fatal("plain key ended capture")
	}
	tt.Key(ui.Alt, ui.KeySpace)
	v.mu.Lock()
	saved, listening := v.settings.Hotkeys.Ocr, v.hotkeyListening
	v.mu.Unlock()
	if saved != "Alt+Space" || listening != "" {
		t.Fatalf("Space activated recorder instead of saving: %q, %q", saved, listening)
	}
}

func TestRecorderUsesMacPhysicalPosition(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Carbon uses physical key positions")
	}
	v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	// The layout reports a different logical key; registration must use the
	// physical key Carbon will bind, just as for Option symbols and IMEs.
	v.captureHotkey("panel", ui.InputEvent{Kind: ui.InputKeyDown, Mods: ui.Alt, Key: ui.KeyZ, PhysicalKey: ui.KeyK})
	v.mu.Lock()
	got := v.settings.Hotkeys.Panel
	v.mu.Unlock()
	if got != "Alt+K" {
		t.Fatalf("captured %q instead of physical Option+K", got)
	}
}
