package platform

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestMacTrayShowsTranslationMode(t *testing.T) {
	// The tray used to show the same mark after switching translation modes.
	var bounds [2]image.Rectangle
	var coverage [2]uint64
	for i, tip := range []string{"Sotto · 气泡模式", "Sotto · 简约模式"} {
		opts := TrayOptions(i == 1)
		if opts.ToolTip != tip || !opts.IconIsTemplate || opts.ID != "main" {
			t.Fatalf("mode %d: tooltip=%q template=%v ID=%q", i, opts.ToolTip, opts.IconIsTemplate, opts.ID)
		}
		img, err := png.Decode(bytes.NewReader(opts.Icon))
		if err != nil {
			t.Fatal(err)
		}
		bounds[i] = img.Bounds()
		for y := bounds[i].Min.Y; y < bounds[i].Max.Y; y++ {
			for x := bounds[i].Min.X; x < bounds[i].Max.X; x++ {
				p := image.Pt(x, y)
				r, g, b, a := img.At(x, y).RGBA()
				if r != 0 || g != 0 || b != 0 {
					t.Fatalf("mode %d: template contains color at %v", i, p)
				}
				if !p.In(img.Bounds().Inset(1)) && a != 0 {
					t.Fatalf("mode %d: template has opaque padding at %v", i, p)
				}
				coverage[i] += uint64(a)
			}
		}
	}
	if bounds[0] != bounds[1] || coverage[1] == 0 || coverage[0] <= coverage[1] {
		t.Fatalf("expected filled and outlined icons on the same canvas: bounds=%v coverage=%v", bounds, coverage)
	}
}
