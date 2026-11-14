package app

import (
	"errors"
	"reflect"
	"sotto/internal/config"
	"testing"
)

func TestMacFallbacksStayOnNativeOptionCombinations(t *testing.T) {
	got := hotkeyFallbacksForPlatform("darwin")
	for action, want := range map[string][]string{"panel": {"Option+A", "Option+Shift+A", "Option+Q"}, "selection": {"Option+D", "Option+Shift+D", "Option+E"}, "ocr": {"Option+S", "Option+Shift+S", "Option+W"}} {
		if !reflect.DeepEqual(got[action], want) {
			t.Fatalf("%s: %v", action, got[action])
		}
	}
}

func TestHotkeyRegistrationFallbackAndRollback(t *testing.T) {
	cfg := config.Default().Hotkeys
	clears := 0
	register := func(action, accel string) error {
		if action == "panel" && accel == cfg.Panel {
			return errors.New("busy")
		}
		return nil
	}
	active, err := registerHotkeys(cfg, nil, register, func() { clears++ })
	if err != nil || active["panel"] == cfg.Panel || len(active) != 3 || clears != 1 {
		t.Fatalf("fallback=%v err=%v", active, err)
	}
	old := map[string]string{"panel": "Alt+F9", "ocr": "Alt+F10"}
	active, err = registerHotkeys(cfg, old, func(action, accel string) error {
		if accel == old[action] {
			return nil
		}
		return errors.New("busy")
	}, func() { clears++ })
	if err == nil || !reflect.DeepEqual(active, old) || clears != 3 {
		t.Fatalf("rollback=%v err=%v", active, err)
	}
	// Failed rollback registrations must not be advertised as active.
	active, err = registerHotkeys(cfg, old, func(string, string) error { return errors.New("busy") }, func() {})
	if err == nil || len(active) != 0 {
		t.Fatal("reported unavailable rollback shortcuts")
	}
}
