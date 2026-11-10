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

// RecoverClipboard only retries by explicit user action and only while the
// failed restore's clipboard sequence still matches. A newer copy always wins.
func RecoverClipboard(id string) error {
	<-clipboardMu
	defer func() { clipboardMu <- struct{}{} }()
	clipboardRecovery.Lock()
	if id == "" || id != clipboardRecovery.id {
		clipboardRecovery.Unlock()
		return ErrClipboardRecoveryGone
	}
	snapshot, sequence := clipboardRecovery.snapshot, clipboardRecovery.sequence
	clipboardRecovery.Unlock()
	owner, cleanup, err := clipboardOwner()
	if err != nil {
		return fmt.Errorf("retry clipboard restore: %w", err)
	}
	defer cleanup()
	prepared, err := prepareClipboardRestore(snapshot)
	if err != nil {
		return fmt.Errorf("prepare clipboard recovery: %w", err)
	}
	defer prepared.free()
	outcome, seq, err := prepared.restore(owner, sequence, func() bool { return clipboardSeq() == sequence })
	if outcome == restoreSkipped {
		return ErrSourceChanged
	}
	clipboardRecovery.Lock()
	defer clipboardRecovery.Unlock()
	clipboardRecovery.sequence = seq
	if err != nil {
		return fmt.Errorf("retry clipboard restore: %w", err)
	}
	clipboardRecovery.id, clipboardRecovery.snapshot = "", nil
	return nil
}
