//go:build windows

package selection

import (
	"runtime"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func newHookTestWindow(t *testing.T) func(parent, style uintptr) uintptr {
	t.Helper()
	// HWND ownership and destruction belong to the creating OS thread. Hidden
	// system-class windows exercise the real owner chain without taking focus.
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	create := modUser32.NewProc("CreateWindowExW")
	destroy := modUser32.NewProc("DestroyWindow")
	var handles []uintptr
	t.Cleanup(func() {
		for i := len(handles) - 1; i >= 0; i-- {
			destroy.Call(handles[i])
		}
	})
	class, err := windows.UTF16FromString("STATIC")
	if err != nil {
		t.Fatal(err)
	}
	return func(parent, style uintptr) uintptr {
		t.Helper()
		hwnd, _, err := create.Call(0, uintptr(unsafe.Pointer(&class[0])), 0, style, 0, 0, 10, 10, parent, 0, 0, 0)
		if hwnd == 0 {
			t.Fatalf("create test window: %v", err)
		}
		handles = append(handles, hwnd)
		return hwnd
	}
}

func TestWindowContainsNativePopup(t *testing.T) {
	window := newHookTestWindow(t)
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
		{"child surface", child, true},
		{"owned popup", popup, true},
		{"owned popup child", popupChild, true},
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

func TestForegroundHookFiltersWindows(t *testing.T) {
	window := newHookTestWindow(t)
	const wsPopup, wsChild = 0x80000000, 0x40000000
	host := window(0, wsPopup)
	bubble := window(host, wsPopup)
	child := window(bubble, wsChild)
	popup := window(bubble, wsPopup)
	popupChild := window(popup, wsChild)
	sibling := window(host, wsPopup)
	other := window(0, wsPopup)
	for _, tt := range []struct {
		name  string
		event uintptr
		hwnd  uintptr
		want  [2]int
	}{
		{"bubble", eventSystemForeground, bubble, [2]int{0, 1}},
		{"child surface", eventSystemForeground, child, [2]int{0, 1}},
		{"owned popup", eventSystemForeground, popup, [2]int{0, 1}},
		{"owned popup child", eventSystemForeground, popupChild, [2]int{0, 1}},
		{"shared owner sibling", eventSystemForeground, sibling, [2]int{1, 1}},
		{"other window", eventSystemForeground, other, [2]int{1, 0}},
		{"no window", eventSystemForeground, 0, [2]int{}},
		{"other event", eventSystemForeground + 1, other, [2]int{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				previous, _ := watch.outside.Load().([]*windowLeaveWatch)
				defer watch.outside.Store(previous)
				var got [2]int
				watch.outside.Store([]*windowLeaveWatch{
					{hwnd: bubble, changed: func() { got[0]++ }},
					{hwnd: other, changed: func() { got[1]++ }},
				})
				foregroundHookProc(0, tt.event, tt.hwnd, 0, 0, 0, 0)
				// Wait for dispatched goroutines without sleeps, including when
				// no callback should have been scheduled for an owned window.
				synctest.Wait()
				if got != tt.want {
					t.Errorf("foreground notifications = %v, want %v", got, tt.want)
				}
			})
		})
	}
}

func TestWatchWindowLeaveForegroundNotifications(t *testing.T) {
	window := newHookTestWindow(t)
	const wsPopup = 0x80000000
	bubble := window(0, wsPopup)
	other := window(0, wsPopup)
	notify := modUser32.NewProc("NotifyWinEvent")
	changes := make(chan struct{}, 1)
	stop, err := WatchWindowLeave(bubble, func() {
		select {
		case changes <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)

	// Hidden windows never activate, so these notifications cannot rely on a
	// WM_ACTIVATE/blur on the bubble. Exercise the real event-hook message pump.
	notify.Call(eventSystemForeground, other, 0, 0)
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("foreground change was not delivered without a bubble blur")
	}

	// Removing one bubble must not stop another bubble's foreground hook.
	second := make(chan struct{}, 1)
	stopSecond, err := WatchWindowLeave(other, func() {
		select {
		case second <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stopSecond)
	stop()
	notify.Call(eventSystemForeground, bubble, 0, 0)
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("unregistering one bubble stopped another bubble's watcher")
	}
	stopSecond()
	watch.mu.Lock()
	stopped := watch.handle == 0 && watch.thread == 0
	watch.mu.Unlock()
	if !stopped {
		t.Fatal("last watcher did not stop the hook thread")
	}
}
