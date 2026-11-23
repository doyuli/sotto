package platform

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestWindowsTrayKeepsBrandColorsAcrossModes(t *testing.T) {
	// Sharing the Mac template replaced the original full-color Windows tray icon.
	var bluePixels [2]int
	for i, tip := range []string{"Sotto · 气泡模式", "Sotto · 简约模式"} {
		opts := TrayOptions(i == 1)
		if opts.IconIsTemplate || opts.ToolTip != tip || opts.ID != "" {
			t.Fatalf("mode %d: template=%v tooltip=%q ID=%q", i, opts.IconIsTemplate, opts.ToolTip, opts.ID)
		}
		img, err := png.Decode(bytes.NewReader(opts.Icon))
		if err != nil {
			t.Fatal(err)
		}
		bounds := img.Bounds()
		if size := bounds.Size(); size != image.Pt(64, 64) {
			t.Fatalf("mode %d: tray icon size = %v, want 64×64", i, size)
		}
		// The old tile covered the taskbar all the way to the canvas edge.
		interior := bounds.Inset(1)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a != 0 && !image.Pt(x, y).In(interior) {
					t.Fatalf("mode %d: nontransparent tray icon edge at (%d, %d)", i, x, y)
				}
				if a == 0xffff && b > r+0x6000 && b > g {
					bluePixels[i]++
				}
			}
		}
	}
	if bluePixels[1] == 0 || bluePixels[1] >= bluePixels[0] {
		t.Fatalf("expected the blue fill to become an outline: blue pixels %v", bluePixels)
	}
}
