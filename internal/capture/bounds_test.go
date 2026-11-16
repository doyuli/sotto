package capture

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestPixelBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		x, y, w, h int64
		wantBytes  int
		limited    bool
	}{
		{"1080p", 0, 0, 1920, 1080, 1920 * 1080 * 4, false},
		{"4k negative origin", -3840, -100, 3840, 2160, 3840 * 2160 * 4, false},
		{"pixel budget boundary", 0, 0, 4096, 4096, 64 << 20, false},
		{"side budget boundary", 0, 0, 16384, 1024, 64 << 20, false},
		{"8k", 0, 0, 7680, 4320, 0, true},
		{"too many pixels", 0, 0, 16384, 1025, 0, true},
		{"too wide", 0, 0, 16385, 1, 0, true},
		{"too tall", 0, 0, 1, 16385, 0, true},
		{"empty", 0, 0, 0, 10, 0, false},
		{"negative height", 0, 0, 10, -1, 0, false},
		{"last Win32 pixel", math.MaxInt32, math.MinInt32, 1, 1, 4, false},
		{"origin overflow", int64(math.MaxInt32) + 1, 0, 1, 1, 0, true},
		{"origin underflow", int64(math.MinInt32) - 1, 0, 1, 1, 0, true},
		{"right overflow", math.MaxInt32, 0, 2, 1, 0, true},
		{"bottom overflow", 0, math.MaxInt32, 1, 2, 0, true},
		{"dimension conversion overflow", 0, 0, int64(math.MaxInt32) + 1, 1, 0, true},
		{"pixel and byte multiplication overflow", 0, 0, math.MaxInt64, math.MaxInt64, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes, err := pixelBytes(tc.x, tc.y, tc.w, tc.h, 16<<20)
			if bytes != tc.wantBytes || (err != nil) != (tc.wantBytes == 0) || errors.Is(err, ErrLimit) != tc.limited {
				t.Fatalf("bytes=%d err=%v; want bytes=%d limited=%t", bytes, err, tc.wantBytes, tc.limited)
			}
		})
	}
}

func TestCaptureUserMessage(t *testing.T) {
	if UserMessage(fmt.Errorf("capture: allocation rejected: %w", ErrLimit)) != "截图范围过大，请缩小框选范围后重试。" {
		t.Fatal("wrapped budget error lost its actionable message")
	}
	if UserMessage(errors.New("GDI failure")) != "截屏失败，请重新框选。" || UserMessage(nil) != "" {
		t.Fatal("unexpected capture error message")
	}
}
