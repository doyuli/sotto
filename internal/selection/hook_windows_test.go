//go:build windows

package selection

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowContainsNativePopup(t *testing.T) {
	// HWND ownership and destruction belong to the creating OS thread. Hidden
	// system-class windows exercise the real owner chain without taking focus.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	create := modUser32.NewProc("CreateWindowExW")
	destroy := modUser32.NewProc("DestroyWindow")
	var handles []uintptr
	defer func() {
		for i := len(handles) - 1; i >= 0; i-- {
			destroy.Call(handles[i])
		}
	}()
	class, err := windows.UTF16FromString("STATIC")
	if err != nil {
		t.Fatal(err)
	}
	window := func(parent, style uintptr) uintptr {
		t.Helper()
		hwnd, _, err := create.Call(0, uintptr(unsafe.Pointer(&class[0])), 0, style, 0, 0, 10, 10, parent, 0, 0, 0)
		if hwnd == 0 {
			t.Fatalf("create test window: %v", err)
		}
		handles = append(handles, hwnd)
		return hwnd
	}
	const wsPopup, wsChild = 0x80000000, 0x40000000
	host := window(0, wsPopup)
	bubble := window(host, wsPopup)
	child := window(bubble, wsChild)
	popup := window(bubble, wsPopup)
	popupChild := window(popup, wsChild)
	sibling := window(host, wsPopup)
	other := window(0, wsPopup)
	for _, tt := range []struct {
		name string
		hit  uintptr
		want bool
	}{
		{"bubble", bubble, true},
		{"webview child", child, true},
		{"select popup", popup, true},
		{"select popup child", popupChild, true},
		{"shared owner sibling", sibling, false},
		{"other window", other, false},
		{"no window", 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowContains(bubble, tt.hit); got != tt.want {
				t.Errorf("windowContains = %t, want %t", got, tt.want)
			}
		})
	}
	if windowContains(0, popup) {
		t.Error("null bubble contains a popup")
	}
}
