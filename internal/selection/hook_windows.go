//go:build windows

package selection

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The double-click watch: a WH_MOUSE_LL hook sees every press on its way
// to whichever window it belongs to, which is what lets a plain double
// click on a word start a translation without a hotkey. The hook's
// thread pumps messages for as long as the watch is on, and the callback
// must answer Windows immediately — every mouse event in the system
// waits for it — so it only tracks clicks and hands the action to a
// goroutine.

const whMouseLL = 14

const (
	wmLButtonDown = 0x0201
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
	procSetWindowsHookExW   = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = modUser32.NewProc("CallNextHookEx")
	procGetMessageW         = modUser32.NewProc("GetMessageW")
	procPostThreadMessageW  = modUser32.NewProc("PostThreadMessageW")
	procGetDoubleClickTime  = modUser32.NewProc("GetDoubleClickTime")
	procGetSystemMetrics    = modUser32.NewProc("GetSystemMetrics")
)

// clickPair is the last press the hook saw.
type clickPair struct {
	down time.Time
	x, y int32
}

// doubleClickWatch is the one hook the process may have. mu guards the
// handle and the click pair; words rides an atomic so Start can swap the
// callback while the hook thread is reading it.
type doubleClickWatch struct {
	mu     sync.Mutex
	handle uintptr
	thread uint32
	done   chan struct{} // closed once the hook thread has unhooked
	words  atomic.Value  // func(), nil-able
	click  clickPair
	// inGrab keeps a second double click during a slow grab from
	// starting another one on top.
	inGrab atomic.Bool
}

var watch doubleClickWatch

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
	watch.mu.Lock()
	running := watch.handle != 0
	watch.mu.Unlock()
	if running {
		return nil
	}
	ready := make(chan error, 1)
	go runDoubleClickHook(ready)
	// Waiting here is safe: the hook goroutine takes only mu.
	return <-ready
}

// stopDoubleClick turns the watch off, waiting for the hook to be gone
// so a following start really starts again.
func stopDoubleClick() {
	startMu.Lock()
	defer startMu.Unlock()
	watch.words.Store(func() {})
	watch.mu.Lock()
	if watch.handle == 0 {
		watch.mu.Unlock()
		return
	}
	thread, done := watch.thread, watch.done
	watch.mu.Unlock()
	procPostThreadMessageW.Call(uintptr(thread), wmQuit, 0, 0)
	<-done
}

// runDoubleClickHook installs the hook and pumps its thread's messages
// until a quit arrives, then unhooks and reports itself gone.
func runDoubleClickHook(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// A low-level hook needs no module handle and calls back into this
	// thread, which the message pump below keeps alive.
	callback := syscall.NewCallback(mouseHookProc)
	h, _, _ := procSetWindowsHookExW.Call(whMouseLL, callback, 0, 0)
	if h == 0 {
		ready <- fmt.Errorf("SetWindowsHookExW: 错误码 %d", h)
		return
	}
	watch.mu.Lock()
	watch.handle = h
	watch.thread = windows.GetCurrentThreadId()
	watch.done = make(chan struct{})
	done := watch.done
	watch.mu.Unlock()
	ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		// 0 on WM_QUIT, -1 on error; both end the pump.
		if r == 0 || m.message == wmQuit {
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
	if code >= 0 && wparam == wmLButtonDown {
		if h := (*msllHookStruct)(native(lparam)); h != nil && h.Flags&llmhfInjected == 0 {
			if f, ok := watch.words.Load().(func()); ok && f != nil {
				watch.noteClick(h.X, h.Y, f)
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wparam, lparam)
	return r
}

// noteClick fires onWord when this press and the previous one form the
// double click Windows itself would report: inside the system's time and
// distance limits. It runs on the hook thread and returns at once; the
// action runs elsewhere.
func (w *doubleClickWatch) noteClick(x, y int32, onWord func()) {
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
