//go:build darwin

package macnative

import (
	"fmt"
	"image"
	"image/draw"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var nativeImage struct {
	createContext  func(unsafe.Pointer, uint64, uint64, uint64, uint64, uintptr, uint32) uintptr
	contextData    func(uintptr) unsafe.Pointer
	contextRelease func(uintptr)
	createImage    func(uintptr) uintptr
	imageRelease   func(uintptr)
	width, height  func(uintptr) uint64
	draw           func(uintptr, Rect, uintptr)
	colorSpace     func(uintptr) uintptr
	space          uintptr
}

var loadImage = sync.OnceValue(func() error {
	if err := Bind("CoreGraphics",
		Symbol{&nativeImage.createContext, "CGBitmapContextCreate"},
		Symbol{&nativeImage.contextData, "CGBitmapContextGetData"},
		Symbol{&nativeImage.contextRelease, "CGContextRelease"},
		Symbol{&nativeImage.createImage, "CGBitmapContextCreateImage"},
		Symbol{&nativeImage.imageRelease, "CGImageRelease"},
		Symbol{&nativeImage.width, "CGImageGetWidth"},
		Symbol{&nativeImage.height, "CGImageGetHeight"},
		Symbol{&nativeImage.draw, "CGContextDrawImage"},
		Symbol{&nativeImage.colorSpace, "CGColorSpaceCreateWithName"},
	); err != nil {
		return err
	}
	lib, err := Framework("CoreGraphics")
	if err != nil {
		return err
	}
	symbol, err := purego.Dlsym(lib, "kCGColorSpaceSRGB")
	if err != nil {
		return fmt.Errorf("resolve sRGB color space: %w", err)
	}
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&symbol))
	nativeImage.space = nativeImage.colorSpace(*(*uintptr)(pointer))
	if nativeImage.space == 0 {
		return fmt.Errorf("create sRGB color space")
	}
	return nil
})

// imageContext owns native pixel memory. No Go buffer is retained by CoreGraphics.
// CoreGraphics' premultiplied-last, big-endian RGBA matches image.RGBA.
func imageContext(width, height int) (uintptr, *image.RGBA, error) {
	if err := loadImage(); err != nil {
		return 0, nil, err
	}
	if width <= 0 || height <= 0 || width > (16<<20)/height {
		return 0, nil, fmt.Errorf("native image dimensions exceed budget")
	}
	const rgba = uint32(1 | 4<<12)
	context := nativeImage.createContext(nil, uint64(width), uint64(height), 8, uint64(width*4), nativeImage.space, rgba)
	if context == 0 {
		return 0, nil, fmt.Errorf("create native image context")
	}
	data := nativeImage.contextData(context)
	if data == nil {
		nativeImage.contextRelease(context)
		return 0, nil, fmt.Errorf("native image context has no pixels")
	}
	pixels := &image.RGBA{Pix: unsafe.Slice((*byte)(data), width*height*4), Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	return context, pixels, nil
}

// CopyNativeImage converts arbitrary CGImage color spaces and row layouts to
// owned sRGB Go pixels. The caller retains the source until this call returns.
func CopyNativeImage(source uintptr) (*image.RGBA, error) {
	if err := loadImage(); err != nil {
		return nil, err
	}
	if source == 0 {
		return nil, fmt.Errorf("empty native image")
	}
	w, h := nativeImage.width(source), nativeImage.height(source)
	if w == 0 || h == 0 || h > 16<<20 || w > (16<<20)/h {
		return nil, fmt.Errorf("native image dimensions exceed budget")
	}
	context, pixels, err := imageContext(int(w), int(h))
	if err != nil {
		return nil, err
	}
	defer nativeImage.contextRelease(context)
	clear(pixels.Pix)
	nativeImage.draw(context, NewRect(0, 0, float64(w), float64(h)), source)
	out := image.NewRGBA(pixels.Rect)
	copy(out.Pix, pixels.Pix)
	return out, nil
}

// WithNativeImage creates a CGImage snapshot for the synchronous consumer and
// releases it afterward. CGBitmapContextCreateImage owns its snapshot even after
// the bitmap context is released; no native reference escapes this scope.
func WithNativeImage(img image.Image, use func(uintptr) error) error {
	if img == nil {
		return fmt.Errorf("empty image")
	}
	bounds := img.Bounds()
	context, pixels, err := imageContext(bounds.Dx(), bounds.Dy())
	if err != nil {
		return err
	}
	draw.Draw(pixels, pixels.Rect, img, bounds.Min, draw.Src)
	native := nativeImage.createImage(context)
	nativeImage.contextRelease(context)
	if native == 0 {
		return fmt.Errorf("create native image snapshot")
	}
	defer nativeImage.imageRelease(native)
	return use(native)
}
