//go:build windows

package capture

import (
	"fmt"
	"image"
	"sync"

	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The GDI screen capture: a screen device context covers the whole
// virtual screen in physical pixels — the process is per-monitor DPI
// aware, as mygo's window layer requires — so one BitBlt takes any
// rectangle, across monitors and their negative coordinates alike.

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")
	modShcore = windows.NewLazySystemDLL("shcore.dll")

	procGetDC               = modUser32.NewProc("GetDC")
	procReleaseDC           = modUser32.NewProc("ReleaseDC")
	procEnumDisplayMonitors = modUser32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = modUser32.NewProc("GetMonitorInfoW")

	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = modGdi32.NewProc("DeleteDC")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
	procSelectObject       = modGdi32.NewProc("SelectObject")
	procBitBlt             = modGdi32.NewProc("BitBlt")

	procGetDpiForMonitor = modShcore.NewProc("GetDpiForMonitor")
)

const (
	biRGB              = 0
	biBitCount         = 32
	dibRGBColors       = 0 // BI_RGB
	srcCopy            = 0x00CC0020
	captureBlt         = 0x40000000
	monitorInfoPrimary = 1
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
	szDevice  [32]uint16
}

// bitmapInfo is BITMAPINFOHEADER followed by no color table, for a
// 32-bpp top-down DIB.
type bitmapInfo struct {
	Size          uint32
	Width         int32
	Height        int32 // negative: top-down, rows in memory top to bottom
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// display is one monitor, in physical pixels with its DPI — the same
// per-display scale mygo's window layer sizes by.
type display struct {
	physX, physY, physW, physH int
	dipX, dipY                 int
	dpi                        int
}

// displays enumerates the monitors. The callback is created once (see
// internal/selection for the same pattern).
var (
	displaySink   func(uintptr)
	displayEnumCb = syscall.NewCallback(func(mon, _, _, _ uintptr) uintptr {
		if displaySink != nil {
			displaySink(mon)
		}
		return 1
	})
)

var displayMu sync.Mutex

func displays() []display {
	displayMu.Lock()
	defer displayMu.Unlock()
	var out []display
	displaySink = func(mon uintptr) {
		var info monitorInfoEx
		info.cbSize = uint32(unsafe.Sizeof(info))
		if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&info))); r == 0 {
			return
		}
		d := display{
			physX: int(info.rcMonitor.Left),
			physY: int(info.rcMonitor.Top),
			physW: int(info.rcMonitor.Right - info.rcMonitor.Left),
			physH: int(info.rcMonitor.Bottom - info.rcMonitor.Top),
			dpi:   96,
		}
		if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&d.dpi)), uintptr(unsafe.Pointer(&d.dpi))); r != 0 || d.dpi == 0 {
			d.dpi = 96
		}
		// mygo's own conversion, so a DIP here is a DIP there.
		d.dipX = (d.physX*96 + d.dpi/2) / d.dpi
		d.dipY = (d.physY*96 + d.dpi/2) / d.dpi
		out = append(out, d)
	}
	procEnumDisplayMonitors.Call(0, 0, displayEnumCb, 0)
	displaySink = nil
	return out
}

// toPx is mygo's DIP-to-physical conversion.
func toPx(v, dpi int) int32 { return int32((v*dpi + 48) / 96) }

// grabDIP converts the DIP rectangle to the physical pixels of the
// display it sits on and captures them.
func grab(r Rect) (image.Image, error) {
	if r.W <= 0 || r.H <= 0 {
		return nil, fmt.Errorf("capture: empty rect %+v", r)
	}
	all := displays()
	if len(all) == 0 {
		return nil, fmt.Errorf("capture: no displays")
	}
	// The display holding the rect's top-left corner decides the scale.
	best := all[0]
	for _, d := range all {
		best = d
		if r.X >= d.dipX && r.X < d.dipX+d.physW*96/d.dpi &&
			r.Y >= d.dipY && r.Y < d.dipY+d.physH*96/d.dpi {
			break
		}
	}
	px := int(toPx(r.X-best.dipX, best.dpi)) + best.physX
	py := int(toPx(r.Y-best.dipY, best.dpi)) + best.physY
	pw := int(toPx(r.W, best.dpi))
	ph := int(toPx(r.H, best.dpi))
	if pw <= 0 || ph <= 0 {
		return nil, fmt.Errorf("capture: rect %+v is sub-pixel on its display", r)
	}
	return bitBlt(px, py, pw, ph)
}

// bitBlt takes pw×ph physical pixels at px,py of the virtual screen.
func bitBlt(px, py, pw, ph int) (image.Image, error) {
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("capture: GetDC(0) failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("capture: CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	header := bitmapInfo{
		Size:     uint32(unsafe.Sizeof(bitmapInfo{})),
		Width:    int32(pw),
		Height:   -int32(ph), // top-down
		Planes:   1,
		BitCount: biBitCount,
	}
	var bits uintptr
	bmp, _, _ := procCreateDIBSection.Call(memDC, uintptr(unsafe.Pointer(&header)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == 0 {
		return nil, fmt.Errorf("capture: CreateDIBSection failed")
	}
	defer procDeleteObject.Call(bmp)

	if prev, _, _ := procSelectObject.Call(memDC, bmp); prev == 0 {
		return nil, fmt.Errorf("capture: SelectObject failed")
	}
	// CAPTUREBLT brings layered windows along, at a flicker cost the
	// one-shot grab can afford.
	if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(pw), uintptr(ph), screenDC, uintptr(int32(px)), uintptr(int32(py)), srcCopy|captureBlt); r == 0 {
		return nil, fmt.Errorf("capture: BitBlt failed")
	}

	img := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	// native through the DIB's memory pointer, as internal/selection
	// does, so go vet sees a legal conversion.
	src := unsafe.Slice((*byte)(native(bits)), pw*ph*4)
	// The DIB's rows are BGRA; GDI leaves the alpha undefined, and the
	// screen is opaque, so every pixel comes out at full opacity.
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 0xff
	}
	return img, nil
}

// native turns a pointer Win32 returned into one Go can look through;
// the same trick mygo's own win32 layer uses to keep go vet content.
func native(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// The overlay has one DPI per display. ClientToScreen preserves the physical
// monitor origin, including negative coordinates, without scaling that origin.
func grabWindow(hwnd uintptr, r Rect) (image.Image, error) {
	if hwnd == 0 || r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 {
		return nil, fmt.Errorf("capture: invalid selection")
	}
	dpi, _, _ := modUser32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		return nil, fmt.Errorf("capture: window DPI unavailable")
	}
	var origin struct{ X, Y int32 }
	if ok, _, _ := modUser32.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&origin))); ok == 0 {
		return nil, fmt.Errorf("capture: window origin unavailable")
	}
	var bounds rect
	if ok, _, _ := modUser32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return nil, fmt.Errorf("capture: window bounds unavailable")
	}
	x, y := toPx(r.X, int(dpi)), toPx(r.Y, int(dpi))
	right, bottom := min(toPx(r.X+r.W, int(dpi)), bounds.Right), min(toPx(r.Y+r.H, int(dpi)), bounds.Bottom)
	if right <= x || bottom <= y {
		return nil, fmt.Errorf("capture: selection outside display")
	}
	return bitBlt(int(origin.X+x), int(origin.Y+y), int(right-x), int(bottom-y))
}
