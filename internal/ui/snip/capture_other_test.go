//go:build !darwin

package snip

import (
	"testing"

	"github.com/egoist/mygo"

	"sotto/internal/capture"
)

func TestWindowCaptureKeepsOverlayLocalCoordinates(t *testing.T) {
	display := mygo.Rectangle{X: -1920, Y: 52, Width: 1920, Height: 1080}
	content := mygo.Rectangle{X: -1908, Y: 77, Width: 1920, Height: 1080}
	r := &Rect{X: 100, Y: 200, W: 300, H: 80}
	if got := captureRect(content, display, r); got != (capture.Rect{X: 100, Y: 200, W: 300, H: 80}) {
		t.Fatalf("applied a display offset before HWND/DPI conversion: %+v", got)
	}
}
