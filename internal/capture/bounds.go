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
// to Win32 types.
func pixelBytes(x, y, w, h int64, maxPixels int) (int, error) {
	if w <= 0 || h <= 0 {
		return 0, fmt.Errorf("capture: empty physical rectangle")
	}
	if x < math.MinInt32 || x > math.MaxInt32 || y < math.MinInt32 || y > math.MaxInt32 ||
		w > math.MaxInt32 || h > math.MaxInt32 || w-1 > math.MaxInt32-x || h-1 > math.MaxInt32-y {
		return 0, fmt.Errorf("%w: physical rectangle exceeds Win32 coordinates", ErrLimit)
	}
	c := pipeline.DefaultConfig()
	if w > int64(c.MaxImageSide) || h > int64(c.MaxImageSide) || w > int64(maxPixels)/h {
		return 0, fmt.Errorf("%w: physical dimensions %dx%d", ErrLimit, w, h)
	}
	maxInt := int64(int(^uint(0) >> 1))
	if w > maxInt/4/h {
		return 0, fmt.Errorf("%w: pixel storage exceeds addressable memory", ErrLimit)
	}
	return int(w * h * 4), nil
}

// ValidateSelection keeps OCR's limits independent of the display snapshot budget.
func ValidateSelection(width, height int) error {
	_, err := pixelBytes(0, 0, int64(width), int64(height), pipeline.DefaultConfig().MaxImagePixels)
	return err
}

func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrLimit) {
		return "截图范围过大，请缩小框选范围后重试"
	}
	return "截屏失败，请重新框选"
}

func SnapshotUserMessage(err error) string {
	if errors.Is(err, ErrLimit) {
		return "屏幕截图过大，请降低显示分辨率或减少启用的显示器后重试"
	}
	return "截屏失败，请重试"
}
