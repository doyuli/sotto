//go:build windows

package platform

import (
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	overlayUser32               = windows.NewLazySystemDLL("user32.dll")
	overlayDwmapi               = windows.NewLazySystemDLL("dwmapi.dll")
	procOverlayGetWindowLongPtr = overlayUser32.NewProc("GetWindowLongPtrW")
	procOverlaySetWindowLongPtr = overlayUser32.NewProc("SetWindowLongPtrW")
	procOverlaySetWindowPos     = overlayUser32.NewProc("SetWindowPos")
	procOverlayDwmSetAttribute  = overlayDwmapi.NewProc("DwmSetWindowAttribute")
)

// ConfigureOverlayWindow strips native window chrome and Windows 11 DWM
// rounding/borders from a transparent overlay panel so only the rendered card
// is visible inside the reserved frame.
func ConfigureOverlayWindow(w *mygo.Window) {
	if w == nil {
		return
	}
	if hwnd := w.NativeHandle(); hwnd != 0 {
		configureOverlayHWND(hwnd)
	}
}

func configureOverlayHWND(hwnd uintptr) {
	const (
		wsCaption uintptr = 0x00C00000

		swpNoSize       = 0x0001
		swpNoMove       = 0x0002
		swpNoZOrder     = 0x0004
		swpNoActivate   = 0x0010
		swpFrameChanged = 0x0020

		dwmwaNCRenderingPolicy      = 2
		dwmwaWindowCornerPreference = 33
		dwmwaBorderColor            = 34

		dwmncrpDisabled  = 1
		dwmwcpDoNotRound = 1
		dwmColorNone     = 0xFFFFFFFE
	)
	gwlStyle := int32(-16)

	if procOverlayGetWindowLongPtr.Find() == nil && procOverlaySetWindowLongPtr.Find() == nil {
		style, _, _ := procOverlayGetWindowLongPtr.Call(hwnd, uintptr(gwlStyle))
		if cleared := style &^ wsCaption; cleared != style {
			procOverlaySetWindowLongPtr.Call(hwnd, uintptr(gwlStyle), cleared)
			if procOverlaySetWindowPos.Find() == nil {
				procOverlaySetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
					swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
			}
		}
	}

	if procOverlayDwmSetAttribute.Find() == nil {
		ncPolicy := int32(dwmncrpDisabled)
		procOverlayDwmSetAttribute.Call(hwnd, dwmwaNCRenderingPolicy, uintptr(unsafe.Pointer(&ncPolicy)), 4)

		cornerPref := int32(dwmwcpDoNotRound)
		procOverlayDwmSetAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&cornerPref)), 4)

		borderColor := uint32(dwmColorNone)
		procOverlayDwmSetAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&borderColor)), 4)
	}
}
