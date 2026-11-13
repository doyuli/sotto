package app

import (
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"

	"sotto/internal/config"
)

// A busy default must not leave an entry action without a shortcut.
var hotkeyActions = []string{"panel", "selection", "ocr"}

var hotkeyLabels = map[string]string{
	"panel":     "翻译面板",
	"selection": "划词翻译",
	"ocr":       "截图翻译",
}

var hotkeyFallbacks = hotkeyFallbacksForPlatform(runtime.GOOS)

func hotkeyFallbacksForPlatform(goos string) map[string][]string {
	if goos == "darwin" {
		return map[string][]string{
			"panel":     {"Option+A", "Option+Shift+A", "Option+Q"},
			"selection": {"Option+D", "Option+Shift+D", "Option+E"},
			"ocr":       {"Option+S", "Option+Shift+S", "Option+W"},
		}
	}
	return map[string][]string{
		"panel":     {"Alt+A", "Alt+Q", "Ctrl+Alt+Q"},
		"selection": {"Alt+D", "Alt+E", "Ctrl+Alt+E"},
		"ocr":       {"Alt+S", "Alt+W", "Ctrl+Alt+W"},
	}
}

// onHotkey is what a registered accelerator runs, on the main thread.
// The actions continue on goroutines, where slow work — a selection
// grab — stays off that thread. ShowTask commits presentation on the UI thread.
func (a *application) onHotkey(action string) func() {
	return func() { a.startEntry(action) }
}

// applyHotkeys switches to the configured shortcuts, falling back when
// one is taken, and rolls back to the previous set when an action
// cannot be registered at all. It holds no lock across the calls that
// wait for the main thread.
func (a *application) applyHotkeys(cfg config.HotkeySettings) error {
	defer a.refreshTrayMenu()
	a.mu.Lock()
	old := make(map[string]string, len(a.hotkeys))
	for action, accel := range a.hotkeys {
		old[action] = accel
	}
	a.mu.Unlock()

	mygo.GlobalShortcut.UnregisterAll()
	active := map[string]string{}
	var firstErr error
	for _, action := range hotkeyActions {
		candidates := hotkeyCandidates(action, cfg)
		registered := ""
		for _, accel := range candidates {
			if err := mygo.GlobalShortcut.Register(accel, a.onHotkey(action)); err == nil {
				registered = accel
				break
			}
		}
		if registered == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("“%s”的候选快捷键（%s）都被占用",
					hotkeyLabels[action], strings.Join(candidates, "、"))
			}
			continue
		}
		if registered != candidates[0] {
			a.notify("Sotto", hotkeyLabels[action]+"的快捷键被占用，已改用 "+registered)
		}
		active[action] = registered
	}
	if firstErr != nil {
		// Put back what worked before this attempt.
		mygo.GlobalShortcut.UnregisterAll()
		for action, accel := range old {
			if err := mygo.GlobalShortcut.Register(accel, a.onHotkey(action)); err != nil {
				log.Printf("rollback hotkey %s: %v", accel, err)
			}
		}
		a.mu.Lock()
		a.hotkeys = old
		a.mu.Unlock()
		a.syncHotkeys(old)
		return firstErr
	}
	a.mu.Lock()
	a.hotkeys = active
	a.mu.Unlock()
	a.syncHotkeys(active)
	return nil
}

// syncHotkeys writes the accelerators actually in force back into the
// settings and broadcasts them. Without this the settings page keeps
// showing the combination the user typed even when registration fell back
// to another accelerator (or rolled back).
func (a *application) syncHotkeys(active map[string]string) {
	a.mu.Lock()
	next := a.cfg.Hotkeys
	if v := active["panel"]; v != "" {
		next.Panel = v
	}
	if v := active["ocr"]; v != "" {
		next.Ocr = v
	}
	if v := active["selection"]; v != "" {
		next.Selection = v
	}
	if next == a.cfg.Hotkeys {
		a.mu.Unlock()
		return
	}
	a.cfg.Hotkeys = next
	cfg := a.cfg
	path := a.cfgPath
	a.mu.Unlock()

	if path != "" {
		if err := config.Save(path, cfg); err != nil {
			log.Printf("save hotkeys: %v", err)
		}
	}
	a.broadcastSettings(cfg)
}

// hotkeyCandidates lists an action's configured accelerator followed by
// its fallbacks, without repeats.
func hotkeyCandidates(action string, cfg config.HotkeySettings) []string {
	configured := map[string]string{
		"panel":     cfg.Panel,
		"selection": cfg.Selection,
		"ocr":       cfg.Ocr,
	}[action]
	var out []string
	for _, accel := range append([]string{configured}, hotkeyFallbacks[action]...) {
		if accel != "" && !slices.Contains(out, accel) {
			out = append(out, accel)
		}
	}
	return out
}
