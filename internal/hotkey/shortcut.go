// Package hotkey owns platform-aware shortcut identities and system registration.
package hotkey

import (
	"fmt"
	"strings"
)

type Modifiers uint8

const (
	Control Modifiers = 1 << iota
	Alt
	Option
	Shift
	Command
	Win
)

type Shortcut struct {
	Mods Modifiers
	Key  string
}

// Parse resolves modifier aliases at the boundary; persisted and registered
// shortcuts use the platform's own names, rather than UI toolkit bit names.
func Parse(value, goos string) (Shortcut, error) {
	var s Shortcut
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "+")
	if strings.HasSuffix(value, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			s.Mods |= Control
		case "alt", "option", "altgr":
			if goos == "darwin" {
				s.Mods |= Option
			} else {
				s.Mods |= Alt
			}
		case "shift":
			s.Mods |= Shift
		case "cmd", "command", "super", "meta", "win":
			if goos == "darwin" {
				s.Mods |= Command
			} else {
				s.Mods |= Win
			}
		case "cmdorctrl", "commandorcontrol":
			if goos == "darwin" {
				s.Mods |= Command
			} else {
				s.Mods |= Control
			}
		default:
			return s, fmt.Errorf("无法识别修饰键 %q", part)
		}
	}
	key := strings.TrimSpace(parts[len(parts)-1])
	for _, name := range Keys(goos) {
		if strings.EqualFold(key, name) {
			s.Key = name
			break
		}
	}
	if s.Key == "" {
		switch strings.ToLower(key) {
		case "return":
			s.Key = "Enter"
		case "esc":
			s.Key = "Escape"
		case "del":
			s.Key = "Delete"
		case "ins":
			s.Key = "Insert"
		case "+", "plus":
			s.Key = "="
			s.Mods |= Shift
		default:
			return s, fmt.Errorf("暂不支持按键 %q，请选择其他按键", key)
		}
	}
	if s.Mods == 0 {
		return s, fmt.Errorf("请至少选择一个修饰键")
	}
	return s, nil
}

func (s Shortcut) String() string {
	var parts []string
	for _, m := range []struct {
		bit  Modifiers
		name string
	}{
		{Control, "Ctrl"}, {Alt, "Alt"}, {Option, "Option"}, {Shift, "Shift"}, {Command, "Cmd"}, {Win, "Win"},
	} {
		if s.Mods&m.bit != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, s.Key), "+")
}

func Canonical(value, goos string) string {
	if s, err := Parse(value, goos); err == nil {
		return s.String()
	}
	return value
}

// Keys is shared by validation and the manual picker, so every offered key
// has a registration path. macOS Carbon only provides F1 through F20.
func Keys(goos string) []string {
	var keys []string
	for c := 'A'; c <= 'Z'; c++ {
		keys = append(keys, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		keys = append(keys, string(c))
	}
	keys = append(keys, "Space", "Enter", "Tab", "Escape", "Backspace", "Delete", "Insert", "Home", "End", "PageUp", "PageDown", "Left", "Right", "Up", "Down", "-", "=", ",", ".", "/", ";", "'", "[", "]", "\\", "`")
	last := 24
	if goos == "darwin" {
		last = 20
	}
	for i := 1; i <= last; i++ {
		keys = append(keys, fmt.Sprintf("F%d", i))
	}
	keys = append(keys, "VolumeUp", "VolumeDown", "VolumeMute", "NumDec", "NumMult", "NumAdd", "NumDiv", "NumSub")
	for i := 0; i <= 9; i++ {
		keys = append(keys, fmt.Sprintf("Num%d", i))
	}
	if goos != "darwin" {
		keys = append(keys, "MediaNextTrack", "MediaPreviousTrack", "MediaStop", "MediaPlayPause", "PrintScreen", "CapsLock", "NumLock", "ScrollLock")
	}
	return keys
}

type Backend interface {
	Register(string, func()) error
	Unregister(string)
	Close()
}
