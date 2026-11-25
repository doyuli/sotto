//go:build windows

package platform

import (
	"os"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

// HistoryWindow applies platform window options for the history window.
// Using Transparent on Windows routes native UI through DirectComposition so
// continuous scrolling does not bypass Windows 11 rounded corner clipping.
func HistoryWindow(o mygo.WindowOptions) mygo.WindowOptions {
	o.Transparent = true
	return SettingsWindow(o)
}

var (
	historyUser32           = windows.NewLazySystemDLL("user32.dll")
	historyWindowProcess    = historyUser32.NewProc("GetWindowThreadProcessId")
	historyReturnForeground = historyUser32.NewProc("SetForegroundWindow")
)

// HistoryFocusReturn captures the source while it is still the foreground
// window. The caller restores it only while the history window owns focus.
func HistoryFocusReturn() func() {
	hwnd := ScreenshotSource()
	pid := historyWindowPID(hwnd)
	if pid == 0 || pid == uint32(os.Getpid()) {
		return nil
	}
	return func() {
		if historyWindowPID(hwnd) == pid {
			historyReturnForeground.Call(hwnd)
		}
	}
}

func historyWindowPID(hwnd uintptr) uint32 {
	var pid uint32
	historyWindowProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}
