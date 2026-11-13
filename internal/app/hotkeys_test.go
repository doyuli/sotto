package app

import (
	"reflect"
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
