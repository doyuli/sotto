//go:build windows

package platform

import (
	"fmt"
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

var (
	selectionButtonComctl32      = windows.NewLazySystemDLL("comctl32.dll")
	procSetSelectionSubclass     = selectionButtonComctl32.NewProc("SetWindowSubclass")
	procRemoveSelectionSubclass  = selectionButtonComctl32.NewProc("RemoveWindowSubclass")
	procDefaultSelectionSubclass = selectionButtonComctl32.NewProc("DefSubclassProc")
	procFindSelectionSurface     = overlayUser32.NewProc("FindWindowExW")
	procGetSelectionClientRect   = overlayUser32.NewProc("GetClientRect")
	procSetSelectionCapture      = overlayUser32.NewProc("SetCapture")
	procReleaseSelectionCapture  = overlayUser32.NewProc("ReleaseCapture")
)

type selectionButtonMouse struct {
	click   func()
	pressed bool
}

// The window and all subclass messages run on the UI thread.
var selectionButtonSurfaces = map[uintptr]*selectionButtonMouse{}

func ConfigureSelectionButtonWindow(w *mygo.Window, click func()) error {
	ConfigureOverlayWindow(w)
	const wsExNoActivate = 0x08000000
	index := int32(-20) // GWL_EXSTYLE
	hwnd := w.NativeHandle()
	style, _, _ := procOverlayGetWindowLongPtr.Call(hwnd, uintptr(index))
	procOverlaySetWindowLongPtr.Call(hwnd, uintptr(index), style|wsExNoActivate)
	current, _, err := procOverlayGetWindowLongPtr.Call(hwnd, uintptr(index))
	if current&wsExNoActivate == 0 {
		return fmt.Errorf("set selection button window style: %w", err)
	}

	// MyGo's native surface calls SetFocus on mouse-down. Handle this
	// one button's mouse click before that path so the source stays active.
	class, _ := windows.UTF16PtrFromString("MyGoSurface")
	surface, _, err := procFindSelectionSurface.Call(hwnd, 0, uintptr(unsafe.Pointer(class)), 0)
	if surface == 0 {
		return fmt.Errorf("find selection button surface: %w", err)
	}
	selectionButtonSurfaces[surface] = &selectionButtonMouse{click: func() { w.Update(click) }}
	for _, handle := range []uintptr{hwnd, surface} {
		if ok, _, err := procSetSelectionSubclass.Call(handle, selectionButtonSubclass, 1, 0); ok == 0 {
			delete(selectionButtonSurfaces, surface)
			return fmt.Errorf("install selection button window subclass: %w", err)
		}
	}
	return nil
}

var selectionButtonSubclass uintptr

func init() {
	selectionButtonSubclass = windows.NewCallback(selectionButtonWindowProc)
}

func selectionButtonWindowProc(hwnd uintptr, message uint32, wp, lp, id, _ uintptr) uintptr {
	const (
		wmMouseActivate  = 0x0021
		wmNCDestroy      = 0x0082
		wmLButtonDown    = 0x0201
		wmLButtonUp      = 0x0202
		wmLButtonDblClk  = 0x0203
		wmCaptureChanged = 0x0215
		maNoActivate     = 3
	)
	switch message {
	case wmMouseActivate:
		return maNoActivate
	case wmNCDestroy:
		delete(selectionButtonSurfaces, hwnd)
		procRemoveSelectionSubclass.Call(hwnd, selectionButtonSubclass, id)
	}
	if mouse := selectionButtonSurfaces[hwnd]; mouse != nil {
		switch message {
		case wmLButtonDown, wmLButtonDblClk:
			mouse.pressed = true
			procSetSelectionCapture.Call(hwnd)
			return 0
		case wmLButtonUp:
			if mouse.pressed {
				mouse.pressed = false
				procReleaseSelectionCapture.Call()
				x, y := int32(int16(lp)), int32(int16(lp>>16))
				var rect struct{ Left, Top, Right, Bottom int32 }
				if ok, _, _ := procGetSelectionClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); ok != 0 &&
					x >= rect.Left && x < rect.Right && y >= rect.Top && y < rect.Bottom {
					mouse.click()
				}
			}
			return 0
		case wmCaptureChanged:
			mouse.pressed = false
		}
	}
	result, _, _ := procDefaultSelectionSubclass.Call(hwnd, uintptr(message), wp, lp)
	return result
}
