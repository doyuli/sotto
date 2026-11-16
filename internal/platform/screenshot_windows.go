//go:build windows

package platform

import "golang.org/x/sys/windows"

var getScreenshotForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("GetForegroundWindow")

// ScreenshotSource identifies the foreground window while a snapshot is pending.
func ScreenshotSource() uintptr {
	hwnd, _, _ := getScreenshotForegroundWindow.Call()
	return hwnd
}
