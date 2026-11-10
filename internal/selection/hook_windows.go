//go:build windows

package selection

import (
	"fmt"
	"log"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A WH_MOUSE_LL hook sees presses before their target window receives
// them. The callback hands off work to a goroutine because delivery of
// mouse input waits for it to return.

const whMouseLL = 14

const eventSystemForeground = 0x0003

const (
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
	wmXButtonDown = 0x020B
	wmQuit        = 0x0012
)

// msllHookStruct is what the low-level mouse hook hands over.
type msllHookStruct struct {
	X         int32
	Y         int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// msg is the MSG structure GetMessageW fills, 48 bytes on amd64; the
// pads place every field where Win32 expects it.
type msg struct {
	hwnd    uintptr
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	_       uint32
}

var (
	procSetWindowsHookExW       = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx     = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx          = modUser32.NewProc("CallNextHookEx")
	procPeekMessageW            = modUser32.NewProc("PeekMessageW")
	procGetMessageW             = modUser32.NewProc("GetMessageW")
	procPostThreadMessageW      = modUser32.NewProc("PostThreadMessageW")
	procWindowFromPhysicalPoint = modUser32.NewProc("WindowFromPhysicalPoint")
	procGetAncestor             = modUser32.NewProc("GetAncestor")
	procGetWindow               = modUser32.NewProc("GetWindow")
	procSetWinEventHook         = modUser32.NewProc("SetWinEventHook")
	procUnhookWinEvent          = modUser32.NewProc("UnhookWinEvent")
)

// windowHooks shares mouse and foreground hooks across visible bubbles.
// The hook can read the callbacks without taking startMu.
type windowHooks struct {
	mu      sync.Mutex
	handle  uintptr
	thread  uint32
	done    chan struct{} // closed once the hook thread has unhooked both hooks
	outside atomic.Value  // immutable []*windowLeaveWatch
}

type windowLeaveWatch struct {
	hwnd    uintptr
	changed func()
}

var watch windowHooks

// startMu serializes starts and stops, so two racing starts cannot
// install duplicate hooks; mu stays for the hook thread's state access.
var startMu sync.Mutex

// WatchWindowLeave calls changed on a goroutine after outside presses and
// foreground changes. The caller rechecks current focus, including owned popups,
// and guards any in-flight callback after unregistering.
func WatchWindowLeave(hwnd uintptr, changed func()) (stop func(), err error) {
	startMu.Lock()
	defer startMu.Unlock()
	if err := ensureWindowHooks(); err != nil {
		return nil, err
	}
	w := &windowLeaveWatch{hwnd: hwnd, changed: changed}
	current, _ := watch.outside.Load().([]*windowLeaveWatch)
	watch.outside.Store(append(slices.Clone(current), w))
	return func() {
		startMu.Lock()
		defer startMu.Unlock()
		current, _ := watch.outside.Load().([]*windowLeaveWatch)
		watch.outside.Store(slices.DeleteFunc(slices.Clone(current), func(candidate *windowLeaveWatch) bool {
			return candidate == w
		}))
		stopWindowHooksIfIdle()
	}, nil
}

// ensureWindowHooks and stopWindowHooksIfIdle run under startMu.
func ensureWindowHooks() error {
	watch.mu.Lock()
	running := watch.handle != 0
	watch.mu.Unlock()
	if running {
		return nil
	}
	ready := make(chan error, 1)
	go runWindowHooks(ready)
	// Waiting here is safe: the hook goroutine takes only mu.
	return <-ready
}

func stopWindowHooksIfIdle() {
	outside, _ := watch.outside.Load().([]*windowLeaveWatch)
	if len(outside) != 0 {
		return
	}
	watch.mu.Lock()
	if watch.handle == 0 {
		watch.mu.Unlock()
		return
	}
	thread, done := watch.thread, watch.done
	watch.mu.Unlock()
	if posted, _, err := procPostThreadMessageW.Call(uintptr(thread), wmQuit, 0, 0); posted == 0 {
		log.Printf("stop mouse hook: PostThreadMessageW: %v", err)
		return
	}
	<-done
}

// runWindowHooks installs both hooks and pumps its thread's messages
// until a quit arrives, then unhooks both and reports itself gone.
func runWindowHooks(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// A low-level hook needs no module handle and calls back into this
	// thread, which the message pump below keeps alive.
	callback := syscall.NewCallback(mouseHookProc)
	h, _, err := procSetWindowsHookExW.Call(whMouseLL, callback, 0, 0)
	if h == 0 {
		ready <- fmt.Errorf("SetWindowsHookExW: %w", err)
		return
	}
	// Out-of-context events arrive on this message-pump thread. Unhook them on
	// the same thread; do not skip our process, since another bubble can activate.
	const wineventOutOfContext = 0
	foreground, _, err := procSetWinEventHook.Call(eventSystemForeground, eventSystemForeground, 0, syscall.NewCallback(foregroundHookProc), 0, 0, wineventOutOfContext)
	if foreground == 0 {
		procUnhookWindowsHookEx.Call(h)
		ready <- fmt.Errorf("SetWinEventHook: %w", err)
		return
	}
	// Create the message queue before the owner can post WM_QUIT.
	var m msg
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, 0)
	watch.mu.Lock()
	watch.handle = h
	watch.thread = windows.GetCurrentThreadId()
	watch.done = make(chan struct{})
	done := watch.done
	watch.mu.Unlock()
	ready <- nil

	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		// 0 on WM_QUIT, -1 on error; both end the pump.
		if int32(r) <= 0 {
			break
		}
	}
	procUnhookWinEvent.Call(foreground)
	procUnhookWindowsHookEx.Call(h)
	watch.mu.Lock()
	watch.handle = 0
	watch.thread = 0
	watch.mu.Unlock()
	close(done)
}

func foregroundHookProc(hook, event, hwnd, object, child, thread, timestamp uintptr) uintptr {
	if event == eventSystemForeground {
		outside, _ := watch.outside.Load().([]*windowLeaveWatch)
		for _, w := range outside {
			go w.changed()
		}
	}
	return 0
}

// mouseHookProc dispatches presses for outside-click detection and
// passes every event on untouched.
func mouseHookProc(code int, wparam uintptr, lparam uintptr) uintptr {
	if code >= 0 {
		switch wparam {
		case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
			h := (*msllHookStruct)(native(lparam))
			if outside, _ := watch.outside.Load().([]*windowLeaveWatch); len(outside) != 0 {
				go notifyOutside(outside, h.X, h.Y)
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, lparam)
	return r
}

func notifyOutside(watches []*windowLeaveWatch, x, y int32) {
	// The hook reports physical screen pixels. Resolve the WebView child
	// to its top-level window before deciding whether the click is outside.
	point := uint64(uint32(x)) | uint64(uint32(y))<<32
	hit, _, _ := procWindowFromPhysicalPoint.Call(uintptr(point))
	for _, w := range watches {
		if !windowContains(w.hwnd, hit) {
			w.changed()
		}
	}
}

// WindowOwnsForeground includes WebView2's native select popup, which has its
// own HWND but belongs to the bubble. Other app windows must still dismiss it.
func WindowOwnsForeground(hwnd uintptr) bool {
	foreground, _, _ := procGetForegroundWindow.Call()
	return windowContains(hwnd, foreground)
}

func windowContains(hwnd, hit uintptr) bool {
	if hwnd == 0 || hit == 0 {
		return false
	}
	if hit == hwnd {
		return true
	}
	const gaRoot, gwOwner = 2, 4
	root, _, _ := procGetAncestor.Call(hit, gaRoot)
	for root != 0 {
		if root == hwnd {
			return true
		}
		root, _, _ = procGetWindow.Call(root, gwOwner)
	}
	return false
}
