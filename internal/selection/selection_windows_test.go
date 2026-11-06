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

func TestToDIP(t *testing.T) {
	// mygo's conversion (internal/windows toDIP), rounding included.
	tests := []struct {
		v    float64
		dpi  int
		want int
	}{
		{0, 96, 0},
		{100, 96, 100},  // (9600+48)/96 = 100.5 → 100
		{150, 144, 100}, // 150% display: (14400+72)/144 = 100.5 → 100
		{200, 96, 200},  // (19200+48)/96 = 200.5 → 200
		{96, 120, 77},   // (9216+60)/120 = 77.3 → 77
	}
	for _, tt := range tests {
		if got := toDIP(tt.v, tt.dpi); got != tt.want {
			t.Errorf("toDIP(%v, %d) = %d, want %d", tt.v, tt.dpi, got, tt.want)
		}
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

func TestCopyBlockers(t *testing.T) {
	// The keys whose holding corrupts a copy. Ctrl must stay out: a copy
	// sent while Ctrl is held is exactly the copy, and waiting for its
	// release would only delay every Ctrl-carrying hotkey.
	for _, vk := range []uint16{vkMenu, vkShift, vkLWin, vkRWin} {
		if !copyBlockers[vk] {
			t.Errorf("vk 0x%X should be a copy blocker", vk)
		}
	}
	if copyBlockers[vkControl] {
		t.Error("vkControl must not be a copy blocker")
	}
}
