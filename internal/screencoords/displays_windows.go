//go:build windows

package screencoords

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	enumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	getMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	getDPIForSystem     = user32.NewProc("GetDpiForSystem")
	getDPIForMonitor    = windows.NewLazySystemDLL("shcore.dll").NewProc("GetDpiForMonitor")
	displaysMu          sync.Mutex
	displaySink         func(uintptr)
	displayCallback     = syscall.NewCallback(func(monitor, _, _, _ uintptr) uintptr {
		if displaySink != nil {
			displaySink(monitor)
		}
		return 1
	})
)

// Current takes a fresh layout snapshot so unplugged displays and scale
// changes cannot leave selection and capture using cached monitor origins.
func Current() Layout {
	displaysMu.Lock()
	defer displaysMu.Unlock()
	fallback := systemDPI()
	var displays []Display
	displaySink = func(monitor uintptr) {
		type rect struct{ left, top, right, bottom int32 }
		var info struct {
			size          uint32
			monitor, work rect
			flags         uint32
			device        [32]uint16
		}
		info.size = uint32(unsafe.Sizeof(info))
		if ok, _, _ := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
			return
		}
		r := info.monitor
		displays = append(displays, Display{
			Pixels: Rect{X: int(r.left), Y: int(r.top), W: int(r.right - r.left), H: int(r.bottom - r.top)},
			DPI:    monitorDPI(monitor, fallback),
		})
	}
	defer func() { displaySink = nil }()
	enumDisplayMonitors.Call(0, 0, displayCallback, 0)
	return NewLayout(displays, fallback)
}

func systemDPI() int {
	if getDPIForSystem.Find() == nil {
		if dpi, _, _ := getDPIForSystem.Call(); dpi != 0 {
			return int(dpi)
		}
	}
	return 96
}

func monitorDPI(monitor uintptr, fallback int) int {
	if monitor != 0 && getDPIForMonitor.Find() == nil {
		var x, y uint32
		if result, _, _ := getDPIForMonitor.Call(monitor, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); result == 0 && x != 0 {
			return int(x)
		}
	}
	return fallback
}
