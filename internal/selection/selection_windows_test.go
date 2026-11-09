//go:build windows

package selection

import (
	"testing"
	"unsafe"
)

func TestKeyboardInputSize(t *testing.T) {
	// SendInput walks an INPUT array by this size; on amd64 it must be
	// exactly 40 bytes or every event after the first lands somewhere
	// else.
	if sz := unsafe.Sizeof(keyboardInput{}); sz != 40 {
		t.Fatalf("keyboardInput is %d bytes, want 40", sz)
	}
}

func TestKeyboardInputLayout(t *testing.T) {
	// The fields the union carries, at the offsets Win32 expects.
	var in keyboardInput
	if off := unsafe.Offsetof(in.WVk); off != 8 {
		t.Errorf("WVk at %d, want 8", off)
	}
	if off := unsafe.Offsetof(in.DwFlags); off != 12 {
		t.Errorf("DwFlags at %d, want 12", off)
	}
	if off := unsafe.Offsetof(in.Extra); off != 24 {
		t.Errorf("Extra at %d, want 24", off)
	}
}

func TestCopyKeyEventsTagEveryEventAndPreserveHeldModifiers(t *testing.T) {
	for _, held := range []bool{false, true} {
		events := copyKeyEvents(copyChord{modifiers: []uint16{vkControl}, key: vkC}, 123, func(uint16) bool { return held })
		want := 4
		if held {
			want = 2
		}
		if len(events) != want {
			t.Fatalf("got %d events, want %d", len(events), want)
		}
		for _, event := range events {
			if event.Extra != 123 {
				t.Fatal("unmarked injected event would cancel copy")
			}
			if held && event.WVk == vkControl {
				t.Fatal("released a physically held modifier")
			}
		}
	}
}

func TestKeyboardHookLayout(t *testing.T) {
	var input keyboardHookInput
	if unsafe.Sizeof(input) != 24 || unsafe.Offsetof(input.extra) != 16 {
		t.Fatal("KBDLLHOOKSTRUCT layout does not match 64-bit Windows")
	}
}

func TestCopyChordFor(t *testing.T) {
	// Ctrl+C is the copy in nearly every app, but not in a terminal. In
	// Windows Terminal even Ctrl+Shift+C reaches the shell as an interrupt
	// when nothing is selected, so there is no safe chord to send.
	tests := []struct {
		class string
		key   uint16
		mods  []uint16
		safe  bool
	}{
		{"ConsoleWindowClass", vkInsert, []uint16{vkControl}, true},
		{"mintty", vkInsert, []uint16{vkControl}, true},
		{"CASCADIA_HOSTING_WINDOW_CLASS", 0, nil, false},
		{"Chrome_WidgetWin_1", vkC, []uint16{vkControl}, true},
	}
	for _, tt := range tests {
		got, safe := copyChordFor(tt.class)
		if safe != tt.safe {
			t.Errorf("copyChordFor(%q) safe = %v, want %v", tt.class, safe, tt.safe)
		}
		if !tt.safe {
			continue
		}
		if got.key != tt.key {
			t.Errorf("copyChordFor(%q).key = 0x%X, want 0x%X", tt.class, got.key, tt.key)
		}
		if len(got.modifiers) != len(tt.mods) {
			t.Errorf("copyChordFor(%q) modifiers = %v, want %v", tt.class, got.modifiers, tt.mods)
			continue
		}
		for i := range tt.mods {
			if got.modifiers[i] != tt.mods[i] {
				t.Errorf("copyChordFor(%q) modifiers = %v, want %v", tt.class, got.modifiers, tt.mods)
				break
			}
		}
	}
}

func TestCopyWaitsForShortcutKeysAndMouseButtons(t *testing.T) {
	for _, held := range []uint16{'D', 0x87, vkControl, 0x12, 0x10, 0x5B, 0x5C, 1, 2, 4, 5, 6} {
		if !copyControlsDown(func(vk uint16) bool { return vk == held }) {
			t.Errorf("held virtual key 0x%X did not block copy", held)
		}
	}
	if copyControlsDown(func(uint16) bool { return false }) {
		t.Error("copy blocked after all keys and buttons were released")
	}
}
