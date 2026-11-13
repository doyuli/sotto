//go:build darwin

package snip

import (
	"testing"

	"github.com/egoist/mygo"

	"sotto/internal/capture"
)

func TestMacCaptureUsesActualOverlayOrigin(t *testing.T) {
	for _, tc := range []struct {
		name             string
		display, content mygo.Rectangle
		want             capture.Rect
	}{
		{"Retina menu bar", mygo.Rectangle{Width: 1470, Height: 956}, mygo.Rectangle{Y: 37, Width: 1470, Height: 956}, capture.Rect{X: 100, Y: 237, W: 300, H: 80}},
		{"external screen on left", mygo.Rectangle{X: -1920, Y: 52, Width: 1920, Height: 1080}, mygo.Rectangle{X: -1920, Y: 77, Width: 1920, Height: 1080}, capture.Rect{X: 100, Y: 225, W: 300, H: 80}},
		{"screen above primary", mygo.Rectangle{Y: -1080, Width: 1920, Height: 1080}, mygo.Rectangle{Y: -1055, Width: 1920, Height: 1080}, capture.Rect{X: 100, Y: 225, W: 300, H: 80}},
		{"horizontal and vertical offset", mygo.Rectangle{X: 1470, Y: -100, Width: 1920, Height: 1080}, mygo.Rectangle{X: 1482, Y: -63, Width: 1920, Height: 1080}, capture.Rect{X: 112, Y: 237, W: 300, H: 80}},
		{"unconstrained overlay", mygo.Rectangle{Width: 1470, Height: 956}, mygo.Rectangle{Width: 1470, Height: 956}, capture.Rect{X: 100, Y: 200, W: 300, H: 80}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection := &Rect{X: 100, Y: 200, W: 300, H: 80}
			got := captureRect(tc.content, tc.display, selection)
			if got != tc.want {
				t.Fatalf("capture=%+v want=%+v", got, tc.want)
			}
			anchor := snipCaptureAnchor(tc.content, selection)
			if anchor == nil || tc.display.X+got.X != tc.content.X+selection.X || tc.display.Y+got.Y+got.H != anchor.Y {
				t.Fatal("screenshot region and bubble anchor disagree about screen coordinates")
			}
		})
	}
}
