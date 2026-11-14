package app

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"runtime"

	"sotto/internal/config"
	"sotto/internal/hotkey"
)

var hotkeyActions = []string{"panel", "selection", "ocr"}
var hotkeyLabels = map[string]string{"panel": "翻译面板", "selection": "划词翻译", "ocr": "截图翻译"}

func hotkeyValues(h config.HotkeySettings) map[string]string {
	return map[string]string{"panel": h.Panel, "selection": h.Selection, "ocr": h.Ocr}
}
func canonicalHotkeys(h config.HotkeySettings) config.HotkeySettings {
	return config.HotkeySettings{Panel: hotkey.Canonical(h.Panel, runtime.GOOS), Selection: hotkey.Canonical(h.Selection, runtime.GOOS), Ocr: hotkey.Canonical(h.Ocr, runtime.GOOS)}
}

// Dispatch by committed accelerator, not by the callback's original action.
// This permits swapping bindings without releasing the working set first.
func (a *application) onShortcut(accel string) func() {
	return func() {
		a.mu.Lock()
		view := a.settingsView
		action := ""
		for id, value := range a.hotkeys {
			if value == accel {
				action = id
				break
			}
		}
		a.mu.Unlock()
		if view != nil && view.CaptureGlobalHotkey(accel) {
			return
		}
		if action != "" {
			a.startEntry(action)
		}
	}
}

// Startup failures remain visible in settings; never replace the user's choice
// with an unrelated fallback or overwrite it just because another app is open.
func (a *application) applyHotkeys(cfg config.HotkeySettings) error {
	active := map[string]string{}
	failures := map[string]string{}
	var errs []error
	values := hotkeyValues(canonicalHotkeys(cfg))
	for _, action := range hotkeyActions {
		accel := values[action]
		err := a.shortcutBackend.Register(accel, a.onShortcut(accel))
		if err != nil {
			failures[action] = err.Error()
			errs = append(errs, fmt.Errorf("%s快捷键 %s：%w", hotkeyLabels[action], accel, err))
		} else {
			active[action] = accel
		}
	}
	a.mu.Lock()
	a.hotkeys, a.hotkeyErrors = active, failures
	a.mu.Unlock()
	return errors.Join(errs...)
}

func (a *application) shortcutErrors() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return maps.Clone(a.hotkeyErrors)
}

func (a *application) updateHotkey(action, value string) error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()
	s := a.settings()
	switch action {
	case "panel":
		s.Hotkeys.Panel = value
	case "selection":
		s.Hotkeys.Selection = value
	case "ocr":
		s.Hotkeys.Ocr = value
	default:
		return fmt.Errorf("未知快捷键操作")
	}
	// Retrying an inactive startup binding must work even if its value is unchanged.
	return a.saveSettingsWithHotkeys(s, action)
}

// commitHotkeys leaves old registrations intact until persistence succeeds.
// Callers serialize mutations; publish must update the callback routing before
// obsolete registrations are released. Unchanged inactive actions stay inactive.
func commitHotkeys(old, desired map[string]string, register func(string) error, unregister func(string), persist func() error, publish func(map[string]string)) error {
	held := map[string]bool{}
	for _, value := range old {
		held[value] = true
	}
	var added []string
	rollback := func() {
		for _, value := range added {
			unregister(value)
		}
	}
	seen := map[string]string{}
	for _, action := range hotkeyActions {
		value := desired[action]
		if value == "" {
			continue
		}
		if owner, exists := seen[value]; exists {
			rollback()
			return fmt.Errorf("%s与%s使用了相同的组合键", hotkeyLabels[action], hotkeyLabels[owner])
		}
		seen[value] = action
		if held[value] {
			continue
		}
		if err := register(value); err != nil {
			rollback()
			return fmt.Errorf("%s：%w", hotkeyLabels[action], err)
		}
		added = append(added, value)
	}
	if err := persist(); err != nil {
		rollback()
		return err
	}
	publish(maps.Clone(desired))
	for value := range held {
		if _, keep := seen[value]; !keep {
			unregister(value)
		}
	}
	return nil
}

func (a *application) saveSettingsWithHotkeys(s config.Settings, retry string) error {
	s.Hotkeys = canonicalHotkeys(s.Hotkeys)
	oldCfg := a.settings()
	previous, requested := hotkeyValues(canonicalHotkeys(oldCfg.Hotkeys)), hotkeyValues(s.Hotkeys)
	a.mu.Lock()
	old := maps.Clone(a.hotkeys)
	a.mu.Unlock()
	desired := maps.Clone(old)
	if desired == nil {
		desired = map[string]string{}
	}
	changed := map[string]bool{}
	for _, action := range hotkeyActions {
		if requested[action] == previous[action] && action != retry {
			continue
		}
		parsed, err := hotkey.Parse(requested[action], runtime.GOOS)
		if err != nil {
			return err
		}
		for other, value := range requested {
			if other != action && value == parsed.String() {
				return fmt.Errorf("该组合键已用于%s，请更换组合键", hotkeyLabels[other])
			}
		}
		desired[action] = parsed.String()
		changed[action] = true
	}
	persist := func() error {
		if err := config.Save(a.cfgPath, s); err != nil {
			return fmt.Errorf("保存设置：%w", err)
		}
		return nil
	}
	if len(changed) != 0 {
		err := commitHotkeys(old, desired, func(value string) error { return a.shortcutBackend.Register(value, a.onShortcut(value)) }, a.shortcutBackend.Unregister, persist, func(active map[string]string) {
			a.mu.Lock()
			a.hotkeys = active
			for action := range changed {
				delete(a.hotkeyErrors, action)
			}
			a.mu.Unlock()
		})
		if err != nil {
			log.Printf("save hotkey: %v", err)
			return err
		}
	} else if err := persist(); err != nil {
		return err
	}
	a.applySettings(s)
	if len(changed) != 0 {
		a.refreshTrayMenu()
	}
	return nil
}
