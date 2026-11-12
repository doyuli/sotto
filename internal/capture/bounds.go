package capture

import (
	"errors"
	"fmt"
	"math"

	"sotto/internal/ocr/pipeline"
)

// ErrLimit identifies selections rejected before allocating screenshot storage.
var ErrLimit = errors.New("capture: resource limit exceeded")

// pixelBytes validates physical coordinates before GDI allocation or narrowing
// to Win32 types. Reuse the OCR input budget so a rejected image is never built.
func pixelBytes(x, y, w, h int64) (int, error) {
	if w <= 0 || h <= 0 {
		return 0, fmt.Errorf("capture: empty physical rectangle")
	}
	if x < math.MinInt32 || x > math.MaxInt32 || y < math.MinInt32 || y > math.MaxInt32 ||
		w > math.MaxInt32 || h > math.MaxInt32 || w-1 > math.MaxInt32-x || h-1 > math.MaxInt32-y {
		return 0, fmt.Errorf("%w: physical rectangle exceeds Win32 coordinates", ErrLimit)
	}
	c := pipeline.DefaultConfig()
	if w > int64(c.MaxImageSide) || h > int64(c.MaxImageSide) || w > int64(c.MaxImagePixels)/h {
		return 0, fmt.Errorf("%w: physical dimensions %dx%d", ErrLimit, w, h)
	}
	maxInt := int64(int(^uint(0) >> 1))
	if w > maxInt/4/h {
		return 0, fmt.Errorf("%w: pixel storage exceeds addressable memory", ErrLimit)
	}
	return int(w * h * 4), nil
}

// windowCaptureRect clips in physical pixels before applying the budget. Local
// edges are rounded as mygo does, without overflowing DIP sums or int32 origins.
func windowCaptureRect(r Rect, dpi int, originX, originY, width, height int32) (Rect, error) {
	if r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 || dpi <= 0 || width <= 0 || height <= 0 {
		return Rect{}, fmt.Errorf("capture: invalid selection geometry")
	}
	maxInt := int(^uint(0) >> 1)
	if r.W > maxInt-r.X || r.H > maxInt-r.Y {
		return Rect{}, fmt.Errorf("%w: selection edges overflow", ErrLimit)
	}
	scale := func(v int, bound int32) int64 {
		return int64(min(float64(bound), math.Floor(float64(v)*float64(dpi)/96+.5)))
	}
	x, y := scale(r.X, width), scale(r.Y, height)
	right, bottom := scale(r.X+r.W, width), scale(r.Y+r.H, height)
	if right <= x || bottom <= y {
		return Rect{}, fmt.Errorf("capture: selection outside display")
	}
	px, py, w, h := int64(originX)+x, int64(originY)+y, right-x, bottom-y
	if _, err := pixelBytes(px, py, w, h); err != nil {
		return Rect{}, err
	}
	return Rect{X: int(px), Y: int(py), W: int(w), H: int(h)}, nil
}

func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrLimit) {
		return "截图范围过大，请缩小框选范围后重试。"
	}
	return "截屏失败，请重新框选。"
}
