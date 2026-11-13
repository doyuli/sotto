package settings

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Keep the stored accelerator in MyGo syntax; symbols are presentation only.
func displayHotkey(accel string) string { return hotkeyDisplayForPlatform(accel, runtime.GOOS) }

func hotkeyDisplayForPlatform(accel, goos string) string {
	if goos != "darwin" {
		return accel
	}
	parts := strings.Split(accel, "+")
	var modifiers, key string
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "ctrl", "control":
			modifiers += "⌃"
		case "alt", "option":
			modifiers += "⌥"
		case "shift":
			modifiers += "⇧"
		case "super", "cmd", "command", "meta", "win", "cmdorctrl", "commandorcontrol":
			modifiers += "⌘"
		default:
			if part == "" {
				key = "+"
			} else {
				key += part
			}
		}
	}
	return modifiers + key
}

func capturedHotkey(mods ui.Modifiers, keyName, goos string) string {
	if keyName == "" || mods&(ui.Ctrl|ui.Alt|ui.Shift|ui.Super) == 0 {
		return ""
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Super != 0 {
		if goos == "darwin" {
			parts = append(parts, "Cmd")
		} else {
			parts = append(parts, "Super")
		}
	}
	return strings.Join(append(parts, keyName), "+")
}
