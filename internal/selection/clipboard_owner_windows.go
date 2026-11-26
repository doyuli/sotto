//go:build windows

package selection

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	procCreateWindowExW = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow   = modUser32.NewProc("DestroyWindow")
)

// Immediate clipboard data remains available after this short-lived owner closes.
// No delayed rendering or UI work is performed on this worker thread.
func clipboardOwner() (uintptr, func(), error) {
	runtime.LockOSThread()
	class, _ := syscall.UTF16PtrFromString("STATIC")
	owner, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, 0,
		0, 0, 0, 0, ^uintptr(2), 0, 0, 0) // HWND_MESSAGE = -3
	if owner == 0 {
		runtime.UnlockOSThread()
		return 0, nil, fmt.Errorf("create clipboard owner: %w", err)
	}
	return owner, func() { procDestroyWindow.Call(owner); runtime.UnlockOSThread() }, nil
}
