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
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A WH_MOUSE_LL hook sees presses before their target window receives
// them. The callback hands off work to a goroutine because delivery of
// mouse input waits for it to return.

const whMouseLL = 14

const (
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
	wmXButtonDown = 0x020B
	wmQuit        = 0x0012
)

// llmhfInjected marks an event a program, not a person, produced:
// synthetic clicks must never read as a user's double click.
const llmhfInjected = 0x1

const (
	smCXDoubleClk = 36
	smCYDoubleClk = 37
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
	procGetDoubleClickTime      = modUser32.NewProc("GetDoubleClickTime")
	procGetSystemMetrics        = modUser32.NewProc("GetSystemMetrics")
	procWindowFromPhysicalPoint = modUser32.NewProc("WindowFromPhysicalPoint")
	procGetAncestor             = modUser32.NewProc("GetAncestor")
)

// clickPair is the last press the hook saw.
type clickPair struct {
	down time.Time
	x, y int32
}

// mouseWatch shares one hook between double-click translation and outside
// click dismissal. The hook can read the callbacks without taking startMu.
type mouseWatch struct {
	mu      sync.Mutex
	handle  uintptr
	thread  uint32
	done    chan struct{} // closed once the hook thread has unhooked
	words   atomic.Value  // func(), nil-able
	outside atomic.Value  // immutable []*outsideClickWatch
	click   clickPair
	// inGrab keeps a second double click during a slow grab from
	// starting another one on top.
	inGrab atomic.Bool
}

type outsideClickWatch struct {
	hwnd    uintptr
	dismiss func()
}

var watch mouseWatch

// startMu serializes starts and stops, so two racing starts cannot
// install two hooks; mu stays for the hook thread's state access.
var startMu sync.Mutex

// startDoubleClick turns the watch on: onWord runs on its own goroutine
// whenever the user double clicks outside an injected event. Starting an
// already running watch only swaps the callback.
func startDoubleClick(onWord func()) error {
	startMu.Lock()
	defer startMu.Unlock()
	watch.words.Store(onWord)
	if err := ensureMouseHook(); err != nil {
		watch.words.Store((func())(nil))
		return err
	}
	return nil
}

func stopDoubleClick() {
	startMu.Lock()
	defer startMu.Unlock()
	watch.words.Store((func())(nil))
	stopMouseHookIfIdle()
}

// WatchOutsideClick calls dismiss on a goroutine when a press lands outside
// hwnd. The caller guards any in-flight callback after unregistering.
func WatchOutsideClick(hwnd uintptr, dismiss func()) (stop func(), err error) {
	startMu.Lock()
	defer startMu.Unlock()
	if err := ensureMouseHook(); err != nil {
		return nil, err
	}
	w := &outsideClickWatch{hwnd: hwnd, dismiss: dismiss}
	current, _ := watch.outside.Load().([]*outsideClickWatch)
	watch.outside.Store(append(slices.Clone(current), w))
	return func() {
		startMu.Lock()
		defer startMu.Unlock()
		current, _ := watch.outside.Load().([]*outsideClickWatch)
		watch.outside.Store(slices.DeleteFunc(slices.Clone(current), func(candidate *outsideClickWatch) bool {
			return candidate == w
		}))
		stopMouseHookIfIdle()
	}, nil
}

// ensureMouseHook and stopMouseHookIfIdle run under startMu.
func ensureMouseHook() error {
	watch.mu.Lock()
	running := watch.handle != 0
	watch.mu.Unlock()
	if running {
		return nil
	}
	ready := make(chan error, 1)
	go runMouseHook(ready)
	// Waiting here is safe: the hook goroutine takes only mu.
	return <-ready
}

func stopMouseHookIfIdle() {
	words, _ := watch.words.Load().(func())
	outside, _ := watch.outside.Load().([]*outsideClickWatch)
	if words != nil || len(outside) != 0 {
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

// runMouseHook installs the hook and pumps its thread's messages
// until a quit arrives, then unhooks and reports itself gone.
func runMouseHook(ready chan<- error) {
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
	procUnhookWindowsHookEx.Call(h)
	watch.mu.Lock()
	watch.handle = 0
	watch.thread = 0
	watch.mu.Unlock()
	close(done)
}

// mouseHookProc is the hook itself: it compares presses and passes
// everything on untouched, whatever it decides.
func mouseHookProc(code int, wparam uintptr, lparam uintptr) uintptr {
	if code >= 0 {
		switch wparam {
		case wmLButtonDown, wmRButtonDown, wmMButtonDown, wmXButtonDown:
			h := (*msllHookStruct)(native(lparam))
			if outside, _ := watch.outside.Load().([]*outsideClickWatch); len(outside) != 0 {
				go dismissOutside(outside, h.X, h.Y)
			}
			if wparam == wmLButtonDown && h.Flags&llmhfInjected == 0 {
				if f, _ := watch.words.Load().(func()); f != nil {
					watch.noteClick(h.X, h.Y, f)
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, lparam)
	return r
}

func dismissOutside(watches []*outsideClickWatch, x, y int32) {
	// The hook reports physical screen pixels. Resolve the WebView child
	// to its top-level window before deciding whether the click is outside.
	point := uint64(uint32(x)) | uint64(uint32(y))<<32
	hit, _, _ := procWindowFromPhysicalPoint.Call(uintptr(point))
	const gaRoot = 2
	root, _, _ := procGetAncestor.Call(hit, gaRoot)
	for _, w := range watches {
		if hit != w.hwnd && root != w.hwnd {
			w.dismiss()
		}
	}
}

// noteClick fires onWord when this press and the previous one form the
// double click Windows itself would report: inside the system's time and
// distance limits. It runs on the hook thread and returns at once; the
// action runs elsewhere.
func (w *mouseWatch) noteClick(x, y int32, onWord func()) {
	w.mu.Lock()
	previous := w.click
	w.click = clickPair{down: time.Now(), x: x, y: y}
	w.mu.Unlock()
	if !isDoubleClick(previous, x, y) {
		return
	}
	// A third press must not read as the next double click's second half.
	w.mu.Lock()
	w.click = clickPair{}
	w.mu.Unlock()
	if w.inGrab.CompareAndSwap(false, true) {
		go func() {
			defer w.inGrab.Store(false)
			onWord()
		}()
	}
}

// isDoubleClick reports whether a press at x, y completes a double click
// started at previous, by the system's own double-click limits.
func isDoubleClick(previous clickPair, x, y int32) bool {
	if previous.down.IsZero() {
		return false
	}
	if time.Since(previous.down) > doubleClickWindow() {
		return false
	}
	if abs32(x-previous.x) > doubleClickSlack(smCXDoubleClk) {
		return false
	}
	return abs32(y-previous.y) <= doubleClickSlack(smCYDoubleClk)
}

// doubleClickWindow is the system's double-click speed, with a little
// slack for the hook running ahead of the click's own delivery.
func doubleClickWindow() time.Duration {
	r, _, _ := procGetDoubleClickTime.Call()
	return time.Duration(r)*time.Millisecond + 50*time.Millisecond
}

// doubleClickSlack is the system's double-click rectangle side, in
// pixels, with the default for a metric that will not load.
func doubleClickSlack(metric uintptr) int32 {
	r, _, _ := procGetSystemMetrics.Call(metric)
	if r == 0 || r > 100 {
		return 4
	}
	return int32(r)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
