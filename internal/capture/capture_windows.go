//go:build windows

package capture

import (
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"sotto/internal/screencoords"

	"golang.org/x/sys/windows"
)

// The GDI screen capture: a screen device context covers the whole
// virtual screen in physical pixels — the process is per-monitor DPI
// aware, as mygo's window layer requires — so one BitBlt takes any
// rectangle, across monitors and their negative coordinates alike.

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC     = modUser32.NewProc("GetDC")
	procReleaseDC = modUser32.NewProc("ReleaseDC")

	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = modGdi32.NewProc("DeleteDC")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
	procSelectObject       = modGdi32.NewProc("SelectObject")
	procBitBlt             = modGdi32.NewProc("BitBlt")
	procGdiFlush           = modGdi32.NewProc("GdiFlush")
)

const (
	biRGB        = 0
	biBitCount   = 32
	dibRGBColors = 0 // BI_RGB
	srcCopy      = 0x00CC0020
	captureBlt   = 0x40000000
)

type rect struct{ Left, Top, Right, Bottom int32 }

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

// toPx is mygo's DIP-to-physical conversion.
func toPx(v, dpi int) int32 { return int32(screencoords.Scale(float64(v), dpi, 96)) }

// grab converts the DIP rectangle through the matching display layout
// before capturing physical screen pixels.
func grab(r Rect) (image.Image, error) {
	if r.W <= 0 || r.H <= 0 {
		return nil, fmt.Errorf("capture: empty rect %+v", r)
	}
	pixels, ok := screencoords.Current().DIPToPixels(screencoords.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
	if !ok {
		return nil, fmt.Errorf("capture: no displays")
	}
	px, py, pw, ph := pixels.X, pixels.Y, pixels.W, pixels.H
	if pw <= 0 || ph <= 0 {
		return nil, fmt.Errorf("capture: rect %+v is sub-pixel on its display", r)
	}
	return bitBlt(px, py, pw, ph)
}

// bitBlt takes pw×ph physical pixels at px,py of the virtual screen.
func bitBlt(px, py, pw, ph int) (image.Image, error) {
	// GDI batches are thread-local; copy, flush and release on the same thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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
	prev, _, _ := procSelectObject.Call(memDC, bmp)
	defer func() {
		// Deselect the bitmap before deleting it.
		if prev != 0 {
			procSelectObject.Call(memDC, prev)
		}
		procDeleteObject.Call(bmp)
	}()
	if prev == 0 {
		return nil, fmt.Errorf("capture: SelectObject failed")
	}
	// CAPTUREBLT brings layered windows along, at a flicker cost the
	// one-shot grab can afford.
	if r, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(pw), uintptr(ph), screenDC, uintptr(int32(px)), uintptr(int32(py)), srcCopy|captureBlt); r == 0 {
		return nil, fmt.Errorf("capture: BitBlt failed")
	}
	if ok, _, _ := procGdiFlush.Call(); ok == 0 {
		return nil, fmt.Errorf("capture: GdiFlush failed")
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
