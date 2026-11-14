package settings

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
	"sotto/internal/config"
	"sotto/internal/hotkey"
)

type hotkeyItem struct{ id, label string }

var settingsHotkeys = []hotkeyItem{{"panel", "翻译面板"}, {"selection", "划词翻译"}, {"ocr", "截图翻译"}}

func displayHotkey(value string) string { return hotkeyDisplayForPlatform(value, runtime.GOOS) }
func hotkeyDisplayForPlatform(value, goos string) string {
	value = hotkey.Canonical(value, goos)
	if goos != "darwin" {
		return value
	}
	return strings.NewReplacer("Ctrl+", "⌃", "Option+", "⌥", "Shift+", "⇧", "Cmd+", "⌘").Replace(value)
}
func capturedHotkey(mods ui.Modifiers, key, goos string) string {
	if key == "" || mods&(ui.Ctrl|ui.Alt|ui.Shift|ui.Super) == 0 {
		return ""
	}
	s := hotkey.Shortcut{Key: key}
	if mods&ui.Ctrl != 0 {
		s.Mods |= hotkey.Control
	}
	if mods&ui.Shift != 0 {
		s.Mods |= hotkey.Shift
	}
	if mods&ui.Alt != 0 {
		if goos == "darwin" {
			s.Mods |= hotkey.Option
		} else {
			s.Mods |= hotkey.Alt
		}
	}
	if mods&ui.Super != 0 {
		if goos == "darwin" {
			s.Mods |= hotkey.Command
		} else {
			s.Mods |= hotkey.Win
		}
	}
	return s.String()
}

func (v *View) CancelHotkeyRecording() {
	v.mu.Lock()
	v.hotkeyListening = ""
	v.mu.Unlock()
	v.invalidate()
}

// Global registrations bypass normal window key-down events. Route our own
// events into the same recorder so conflicts never trigger a translation.
func (v *View) CaptureGlobalHotkey(value string) bool {
	v.mu.Lock()
	if v.disposed || v.activeTab != TabHotkeys || v.hotkeyListening == "" {
		v.mu.Unlock()
		return false
	}
	v.submitHotkeyLocked(v.hotkeyListening, value)
	v.mu.Unlock()
	v.invalidate()
	return true
}

func (v *View) captureHotkey(id string, ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown || ev.Repeat {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hotkeyListening != id || v.activeTab != TabHotkeys {
		return false
	}
	if ev.Key == ui.KeyEscape && ev.Mods == 0 {
		v.hotkeyListening = ""
		v.hotkeyConflicts[id] = ""
		return true
	}
	// These keypad positions have no distinct supported accelerator. Falling
	// back to their logical key would bind main Return/= instead of this key.
	// Windows intentionally shares VK_RETURN between its two Enter keys.
	if ev.Code == ui.CodeNumpadEqual || ev.Code == ui.CodeNumpadComma ||
		(runtime.GOOS == "darwin" && ev.Code == ui.CodeNumpadEnter) {
		v.hotkeyConflicts[id] = "暂不支持这个小键盘按键，请使用主键盘上的对应按键"
		return true
	}
	key := ev.Key
	if runtime.GOOS == "darwin" && ev.PhysicalKey != ui.KeyUnknown {
		key = ev.PhysicalKey
	}
	name := formatHotkeyKey(key)
	if extended := extendedHotkeyKey(ev.Code); extended != "" {
		name = extended
	}
	// Modifier-only events have no main key yet; keep waiting without an error.
	if name == "" {
		if ev.Code >= ui.CodeControlLeft && ev.Code <= ui.CodeSuperRight {
			return true
		}
		v.hotkeyConflicts[id] = "暂不支持这个按键，请换一个按键或使用手动设置"
		return true
	}
	value := capturedHotkey(ev.Mods, name, runtime.GOOS)
	if value == "" {
		v.hotkeyConflicts[id] = "请同时按住至少一个修饰键，再按下主键"
		return true
	}
	v.submitHotkeyLocked(id, value)
	return true
}

// KeyCode retains keypad identity and function keys absent from ui.Key.
func extendedHotkeyKey(code ui.KeyCode) string {
	if code >= ui.CodeF13 && code <= ui.CodeF24 {
		return fmt.Sprintf("F%d", code-ui.CodeF13+13)
	}
	if code >= ui.CodeNumpad1 && code <= ui.CodeNumpad9 {
		return fmt.Sprintf("Num%d", code-ui.CodeNumpad1+1)
	}
	return map[ui.KeyCode]string{
		ui.CodeNumpad0: "Num0", ui.CodeNumpadDecimal: "NumDec",
		ui.CodeNumpadAdd: "NumAdd", ui.CodeNumpadSubtract: "NumSub",
		ui.CodeNumpadMultiply: "NumMult", ui.CodeNumpadDivide: "NumDiv",
		ui.CodeAudioVolumeMute: "VolumeMute", ui.CodeAudioVolumeUp: "VolumeUp", ui.CodeAudioVolumeDown: "VolumeDown",
	}[code]
}

func (v *View) submitHotkeyLocked(id, value string) {
	if v.disposed || v.hotkeyPending != "" {
		return
	}
	parsed, err := hotkey.Parse(value, runtime.GOOS)
	if err != nil {
		v.hotkeyConflicts[id] = err.Error()
		return
	}
	value = parsed.String()
	for _, item := range settingsHotkeys {
		if item.id != id && hotkey.Canonical(hotkeyValue(v.settings.Hotkeys, item.id), runtime.GOOS) == value {
			v.hotkeyConflicts[id] = "该组合键已用于" + item.label + "，请换一个组合键"
			return
		}
	}
	v.hotkeyListening = ""
	v.hotkeyConflicts[id] = ""
	v.hotkeyPending = id
	v.pendingWrites++
	generation := v.windowGeneration
	v.enqueueTaskLocked(func() {
		var err error
		if v.actions.SaveHotkey != nil {
			err = v.actions.SaveHotkey(id, value)
		} else if v.actions.SaveSettings != nil {
			v.mu.Lock()
			next := cloneSettings(v.settings)
			v.mu.Unlock()
			if v.actions.GetSettings != nil {
				next = v.actions.GetSettings()
			}
			setHotkeyValue(&next.Hotkeys, id, value)
			err = v.actions.SaveSettings(next)
		}
		var current *config.Settings
		if v.actions.GetSettings != nil {
			cfg := v.actions.GetSettings()
			current = &cfg
		}
		v.mu.Lock()
		v.pendingWrites--
		v.hotkeyPending = ""
		v.saveFailed = err != nil
		if current != nil {
			if v.pendingWrites == 0 {
				v.settings = cloneSettings(*current)
			} else {
				v.settings.Hotkeys = current.Hotkeys
			}
		} else if err == nil {
			setHotkeyValue(&v.settings.Hotkeys, id, value)
		}
		if !v.disposed && generation == v.windowGeneration {
			if err != nil {
				v.hotkeyConflicts[id] = err.Error()
			} else {
				v.hotkeyConflicts[id] = ""
				v.hotkeyEditor = ""
			}
		}
		disposed := v.disposed
		v.mu.Unlock()
		if !disposed {
			v.invalidate()
		}
	})
}

func hotkeyValue(h config.HotkeySettings, id string) string {
	switch id {
	case "panel":
		return h.Panel
	case "selection":
		return h.Selection
	case "ocr":
		return h.Ocr
	}
	return ""
}
func setHotkeyValue(h *config.HotkeySettings, id, value string) {
	switch id {
	case "panel":
		h.Panel = value
	case "selection":
		h.Selection = value
	case "ocr":
		h.Ocr = value
	}
}
func formatHotkeyKey(key ui.Key) string {
	switch {
	case key >= ui.KeyA && key <= ui.KeyZ:
		return string(rune('A' + key - ui.KeyA))
	case key >= ui.Key0 && key <= ui.Key9:
		return string(rune('0' + key - ui.Key0))
	case key >= ui.KeyF1 && key <= ui.KeyF12:
		return fmt.Sprintf("F%d", key-ui.KeyF1+1)
	}
	return map[ui.Key]string{
		ui.KeySpace: "Space", ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeyEscape: "Escape", ui.KeyBackspace: "Backspace", ui.KeyDelete: "Delete",
		ui.KeyInsert: "Insert", ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
		ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down", ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".",
		ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]", ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
	}[key]
}
