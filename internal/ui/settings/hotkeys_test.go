package settings

import (
	"errors"
	"runtime"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"sotto/internal/config"
)

func TestPlatformShortcutDisplayAndCapture(t *testing.T) {
	for _, tc := range []struct{ value, mac, win string }{
		{"Alt+A", "⌥A", "Alt+A"}, {"Option+Shift+S", "⌥⇧S", "Alt+Shift+S"},
		{"Cmd+K", "⌘K", "Win+K"}, {"Ctrl+Alt+Shift+Cmd+K", "⌃⌥⇧⌘K", "Ctrl+Alt+Shift+Win+K"},
		{"CmdOrCtrl++", "⇧⌘=", "Ctrl+Shift+="},
	} {
		if got := hotkeyDisplayForPlatform(tc.value, "darwin"); got != tc.mac {
			t.Errorf("mac: %s != %s", got, tc.mac)
		}
		if got := hotkeyDisplayForPlatform(tc.value, "windows"); got != tc.win {
			t.Errorf("win: %s != %s", got, tc.win)
		}
	}
	if got := capturedHotkey(ui.Alt|ui.Super, "K", "darwin"); got != "Option+Cmd+K" {
		t.Fatal(got)
	}
	if got := capturedHotkey(ui.Alt|ui.Super, "K", "windows"); got != "Alt+Win+K" {
		t.Fatal(got)
	}
}

func TestRecorderCommitsKeyDownWithoutNextFrame(t *testing.T) {
	func() {
		v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
		defer v.Dispose()
		tt := ui.NewTester(v.Render, 780, 600)
		if err := tt.Click("翻译面板快捷键"); err != nil {
			t.Fatal(err)
		}
		if !tt.Focused("翻译面板快捷键") {
			t.Fatal("recorder has no focus")
		}
		for _, kind := range []ui.InputKind{ui.InputKeyDown, ui.InputKeyUp} {
			v.captureHotkey("panel", ui.InputEvent{Kind: kind, Mods: ui.Ctrl | ui.Shift, Key: ui.KeyK})
		}
		drainSettingsTasks(v)
		if v.settings.Hotkeys.Panel != "Ctrl+Shift+K" || v.hotkeyListening != "" {
			t.Fatalf("saved=%s recording=%s", v.settings.Hotkeys.Panel, v.hotkeyListening)
		}
	}()
}

func TestRecorderSupportsNavigationPunctuationAndSpace(t *testing.T) {
	for _, tc := range []struct {
		key  ui.Key
		name string
	}{{ui.KeySpace, "Space"}, {ui.KeyLeft, "Left"}, {ui.KeyTab, "Tab"}, {ui.KeyEnter, "Enter"}, {ui.KeySemicolon, ";"}, {ui.KeyEqual, "="}, {ui.KeyBackslash, "\\"}} {
		t.Run(tc.name, func(t *testing.T) {
			func() {
				v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
				defer v.Dispose()
				tt := ui.NewTester(v.Render, 780, 600)
				if err := tt.Click("截图翻译快捷键"); err != nil {
					t.Fatal(err)
				}
				tt.Key(0, ui.KeyK)
				if !tt.HasText("请同时按住至少一个修饰键，再按下主键") {
					t.Fatal("plain key was silently ignored")
				}
				tt.Key(ui.Alt, tc.key)
				drainSettingsTasks(v)
				if got := v.settings.Hotkeys.Ocr; got != capturedHotkey(ui.Alt, tc.name, runtime.GOOS) {
					t.Fatal(got)
				}
			}()
		})
	}
}

func TestRecorderReportsGlobalConflictAndAcceptsNextAttempt(t *testing.T) {
	func() {
		cfg := testSettingsConfig()
		v := NewView(TabHotkeys, cfg, Actions{})
		defer v.Dispose()
		tt := ui.NewTester(v.Render, 780, 600)
		if err := tt.Click("翻译面板快捷键"); err != nil {
			t.Fatal(err)
		}
		// The OS delivers our registered shortcut through its global callback.
		if !v.CaptureGlobalHotkey(cfg.Hotkeys.Selection) {
			t.Fatal("global event escaped the recorder")
		}
		tt.Frame()
		if !tt.HasText("该组合键已用于划词翻译，请换一个组合键") || v.hotkeyListening != "panel" {
			t.Fatal(tt.Texts())
		}
		tt.Key(ui.Ctrl|ui.Shift, ui.KeyK)
		drainSettingsTasks(v)
		if v.settings.Hotkeys.Panel != "Ctrl+Shift+K" {
			t.Fatal(v.settings.Hotkeys)
		}
		if v.CaptureGlobalHotkey(cfg.Hotkeys.Selection) {
			t.Fatal("global shortcut remained intercepted after recording")
		}
	}()
}

func TestRecorderFailureKeepsOldValueAndAllowsRetry(t *testing.T) {
	func() {
		current := testSettingsConfig()
		old := current.Hotkeys.Panel
		release := make(chan struct{})
		started := make(chan struct{})
		calls := 0
		v := NewView(TabHotkeys, current, Actions{
			GetSettings: func() config.Settings { return current },
			SaveHotkey: func(id, value string) error {
				calls++
				if calls == 1 {
					close(started)
					<-release
					return errors.New("该组合键已被其他应用占用，请更换组合键")
				}
				setHotkeyValue(&current.Hotkeys, id, value)
				return nil
			},
		})
		defer v.Dispose()
		tt := ui.NewTester(v.Render, 780, 600)
		if err := tt.Click("翻译面板快捷键"); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Ctrl|ui.Shift, ui.KeyK)
		<-started
		tt.Frame()
		if !tt.HasText("正在保存…") || v.settings.Hotkeys.Panel != old {
			t.Fatal("advertised pending shortcut as active")
		}
		close(release)
		drainSettingsTasks(v)
		tt.Frame()
		if !tt.HasText("该组合键已被其他应用占用，请更换组合键") || v.settings.Hotkeys.Panel != old {
			t.Fatal(tt.Texts())
		}
		if err := tt.Click("翻译面板快捷键"); err != nil {
			t.Fatal(err)
		}
		tt.Key(ui.Ctrl|ui.Shift, ui.KeyL)
		drainSettingsTasks(v)
		tt.Frame()
		if v.settings.Hotkeys.Panel != "Ctrl+Shift+L" {
			t.Fatal(v.settings.Hotkeys)
		}
	}()
}

func TestRecorderCancelsOnBlurAndWindowDetach(t *testing.T) {
	for _, detach := range []bool{false, true} {
		t.Run(map[bool]string{false: "blur", true: "detach"}[detach], func(t *testing.T) {
			v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
			defer v.Dispose()
			tt := ui.NewTester(v.Render, 780, 600)
			if err := tt.Click("翻译面板快捷键"); err != nil {
				t.Fatal(err)
			}
			if detach {
				v.DetachWindow()
			} else {
				v.CancelHotkeyRecording()
			}
			if v.CaptureGlobalHotkey("Ctrl+K") {
				t.Fatal("hidden recorder consumed a global shortcut")
			}
		})
	}
}

func TestManualHotkeyPickerSavesWithoutKeyCapture(t *testing.T) {
	func() {
		cfg := testSettingsConfig()
		cfg.Hotkeys.Panel = "Ctrl+K"
		v := NewView(TabHotkeys, cfg, Actions{})
		defer v.Dispose()
		tt := ui.NewTester(v.Render, 660, 600)
		if err := tt.Click("翻译面板手动设置"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("翻译面板修饰键Shift"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("翻译面板主键"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyDown)
		tt.Key(0, ui.KeyEnter)
		if err := tt.Click("翻译面板保存快捷键"); err != nil {
			t.Fatal(err)
		}
		drainSettingsTasks(v)
		tt.Frame()
		if v.settings.Hotkeys.Panel != "Ctrl+Shift+L" || v.hotkeyEditor != "" {
			t.Fatalf("settings=%v editor=%s", v.settings.Hotkeys, v.hotkeyEditor)
		}
	}()
}

// A FIFO barrier observes completed writes without relying on frame timing.
func drainSettingsTasks(v *View) {
	done := make(chan struct{})
	v.enqueueTask(func() { close(done) })
	<-done
}

func TestHotkeySaveAndFollowingSettingsEditPreserveBothChanges(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	started, release := make(chan struct{}), make(chan struct{})
	v := NewView(TabHotkeys, current, Actions{
		GetSettings: func() config.Settings { mu.Lock(); defer mu.Unlock(); return current },
		SaveHotkey: func(id, value string) error {
			close(started)
			<-release
			mu.Lock()
			setHotkeyValue(&current.Hotkeys, id, value)
			mu.Unlock()
			return nil
		},
		SaveSettings: func(next config.Settings) error { mu.Lock(); current = next; mu.Unlock(); return nil },
	})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyK)
	<-started
	v.mu.Lock()
	next := v.settings
	next.TargetLanguage = "en"
	v.enqueueSaveLocked(next)
	v.mu.Unlock()
	close(release)
	drainSettingsTasks(v)
	mu.Lock()
	defer mu.Unlock()
	if current.Hotkeys.Panel != "Ctrl+Shift+K" || current.TargetLanguage != "en" {
		t.Fatalf("lost queued changes: %+v", current)
	}
}

func TestHotkeyFailureAfterReopenDoesNotReopenEditor(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	v := NewView(TabHotkeys, testSettingsConfig(), Actions{SaveHotkey: func(string, string) error { close(started); <-release; return errors.New("旧窗口保存失败") }})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyK)
	<-started
	v.DetachWindow()
	v.SetActiveTab(TabHotkeys)
	close(release)
	drainSettingsTasks(v)
	tt = ui.NewTester(v.Render, 780, 600)
	if tt.HasText("旧窗口保存失败") || v.hotkeyEditor != "" || v.hotkeyListening != "" {
		t.Fatal("stale result changed a reopened editor")
	}
}

func TestStartupHotkeyFailureRemainsVisibleAfterCancelAndReopen(t *testing.T) {
	v := NewView(TabHotkeys, testSettingsConfig(), Actions{HotkeyErrors: func() map[string]string { return map[string]string{"panel": "该组合键已被占用"} }})
	defer v.Dispose()
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if !tt.HasText("未生效：该组合键已被占用") {
		t.Fatal("cancel hid inactive binding status")
	}
	v.DetachWindow()
	v.SetActiveTab(TabHotkeys)
	tt = ui.NewTester(v.Render, 780, 600)
	if !tt.HasText("未生效：该组合键已被占用") {
		t.Fatal("reopen hid inactive binding status")
	}
}

func TestRecorderPreservesKeypadAndExtendedFunctionKeys(t *testing.T) {
	for _, tc := range []struct {
		code ui.KeyCode
		key  ui.Key
		want string
	}{
		{ui.CodeNumpad1, ui.Key1, "Num1"}, {ui.CodeNumpadAdd, ui.KeyEqual, "NumAdd"}, {ui.CodeF13, ui.KeyUnknown, "F13"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
			defer v.Dispose()
			tt := ui.NewTester(v.Render, 780, 600)
			if err := tt.Click("翻译面板快捷键"); err != nil {
				t.Fatal(err)
			}
			v.captureHotkey("panel", ui.InputEvent{Kind: ui.InputKeyDown, Key: tc.key, Code: tc.code, Mods: ui.Ctrl})
			drainSettingsTasks(v)
			if v.settings.Hotkeys.Panel != "Ctrl+"+tc.want {
				t.Fatal(v.settings.Hotkeys.Panel)
			}
		})
	}
}

func TestRecorderRejectsUnsupportedKeypadPositions(t *testing.T) {
	cases := []struct {
		code ui.KeyCode
		key  ui.Key
	}{
		{ui.CodeNumpadEqual, ui.KeyEqual},
		{ui.CodeNumpadComma, ui.KeyComma},
	}
	if runtime.GOOS == "darwin" {
		cases = append(cases, struct {
			code ui.KeyCode
			key  ui.Key
		}{ui.CodeNumpadEnter, ui.KeyEnter})
	}
	for _, tc := range cases {
		v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
		t.Cleanup(v.Dispose)
		old := v.settings.Hotkeys.Panel
		tt := ui.NewTester(v.Render, 780, 600)
		if err := tt.Click("翻译面板快捷键"); err != nil {
			t.Fatal(err)
		}
		// A keypad Enter used to silently register the main Return key on Mac.
		v.captureHotkey("panel", ui.InputEvent{Kind: ui.InputKeyDown, Key: tc.key, Code: tc.code, Mods: ui.Ctrl})
		drainSettingsTasks(v)
		tt.Frame()
		if !tt.HasText("暂不支持这个小键盘按键，请使用主键盘上的对应按键") || v.settings.Hotkeys.Panel != old || v.hotkeyListening != "panel" {
			t.Fatalf("keypad code %v: binding=%s recording=%s texts=%v", tc.code, v.settings.Hotkeys.Panel, v.hotkeyListening, tt.Texts())
		}
		tt.Key(ui.Ctrl, ui.KeyEnter)
		drainSettingsTasks(v)
		if v.settings.Hotkeys.Panel != "Ctrl+Enter" {
			t.Fatal("recorder did not recover with a supported main key")
		}
	}
}

func TestHotkeySaveReconcilesEarlierFailedSettingsWrite(t *testing.T) {
	var mu sync.Mutex
	current := testSettingsConfig()
	started, release := make(chan struct{}), make(chan struct{})
	v := NewView(TabHotkeys, current, Actions{
		GetSettings:  func() config.Settings { mu.Lock(); defer mu.Unlock(); return current },
		SaveSettings: func(config.Settings) error { close(started); <-release; return errors.New("disk full") },
		SaveHotkey: func(id, value string) error {
			mu.Lock()
			setHotkeyValue(&current.Hotkeys, id, value)
			mu.Unlock()
			return nil
		},
	})
	defer v.Dispose()
	v.mu.Lock()
	next := v.settings
	next.TargetLanguage = "en"
	v.enqueueSaveLocked(next)
	v.mu.Unlock()
	<-started
	tt := ui.NewTester(v.Render, 780, 600)
	if err := tt.Click("翻译面板快捷键"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Ctrl|ui.Shift, ui.KeyK)
	close(release)
	drainSettingsTasks(v)
	if v.settings.TargetLanguage != "zh-HANS" || v.settings.Hotkeys.Panel != "Ctrl+Shift+K" {
		t.Fatalf("stale optimistic settings: %+v", v.settings)
	}
}
