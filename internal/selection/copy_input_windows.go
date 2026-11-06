//go:build windows

package selection

import (
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const whKeyboardLL = 13

type keyboardHookInput struct {
	key, scan, flags, time uint32
	extra                  uintptr
}

type copyInputWatch struct {
	copyInputState
	thread uint32
	done   chan struct{}
}

var (
	copyInputSerial      atomic.Uint32
	activeCopyInput      atomic.Pointer[copyInputState]
	copyKeyboardCallback = syscall.NewCallback(copyKeyboardHook)
	copyMouseCallback    = syscall.NewCallback(copyMouseHook)
)

// clipboardMu serializes callers. Observe input only during the copy transaction,
// using a dedicated message-pump thread; never block either input hook callback.
func watchCopyInput() (*copyInputWatch, error) {
	w := &copyInputWatch{copyInputState: copyInputState{marker: uintptr(0x534f00000000) + uintptr(copyInputSerial.Add(1))}, done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(w.done)
		defer w.changed.Store(true)
		var message msg
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 0)
		w.thread = windows.GetCurrentThreadId()
		if !activeCopyInput.CompareAndSwap(nil, &w.copyInputState) {
			ready <- errors.New("copy input watcher is still shutting down")
			return
		}
		defer activeCopyInput.CompareAndSwap(&w.copyInputState, nil)
		keyboard, _, err := procSetWindowsHookExW.Call(whKeyboardLL, copyKeyboardCallback, 0, 0)
		if keyboard == 0 {
			ready <- fmt.Errorf("watch copy keyboard: %w", err)
			return
		}
		defer procUnhookWindowsHookEx.Call(keyboard)
		mouse, _, err := procSetWindowsHookExW.Call(whMouseLL, copyMouseCallback, 0, 0)
		if mouse == 0 {
			ready <- fmt.Errorf("watch copy mouse: %w", err)
			return
		}
		defer procUnhookWindowsHookEx.Call(mouse)
		ready <- nil
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
		}
	}()
	if err := <-ready; err != nil {
		<-w.done
		return nil, err
	}
	return w, nil
}

func (w *copyInputWatch) stop() {
	select {
	case <-w.done:
		return
	default:
	}
	if ok, _, err := procPostThreadMessageW.Call(uintptr(w.thread), wmQuit, 0, 0); ok == 0 {
		w.changed.Store(true)
		log.Printf("stop copy input watch: %v", err)
		return
	}
	<-w.done
}

func copyKeyboardHook(code int, wp, lp uintptr) uintptr {
	if code >= 0 {
		if state := activeCopyInput.Load(); state != nil {
			input := (*keyboardHookInput)(native(lp))
			state.observe(input.flags&0x10 != 0, input.extra) // LLKHF_INJECTED
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wp, lp)
	return r
}

func copyMouseHook(code int, wp, lp uintptr) uintptr {
	if code >= 0 {
		if state := activeCopyInput.Load(); state != nil {
			input := (*msllHookStruct)(native(lp))
			state.observe(input.Flags&1 != 0, input.ExtraInfo) // LLMHF_INJECTED
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wp, lp)
	return r
}
