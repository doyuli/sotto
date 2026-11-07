package ocr

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
)

func TestSystemResizeMapsActualDimensions(t *testing.T) {
	src := image.NewNRGBA(image.Rect(50, 70, 1050, 571))
	dst, err := fitSystemImage(context.Background(), src, 500)
	if err != nil {
		t.Fatal(err)
	}
	if dst.Bounds() != image.Rect(0, 0, 500, 250) {
		t.Fatal(dst.Bounds())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fitSystemImage(ctx, src, 500); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := resizeBilinear(ctx, src, 10, 10); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestResizeBilinearKeepsCorners(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	src.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	src.SetNRGBA(1, 1, color.NRGBA{A: 255})
	dst, err := resizeBilinear(context.Background(), src, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if b := dst.Bounds(); b.Dx() != 4 || b.Dy() != 4 {
		t.Fatalf("size = %v, want 4x4", b)
	}
	// The corners take their source pixel's colour.
	if c := dst.NRGBAAt(0, 0); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("top-left = %+v, want red", c)
	}
	if c := dst.NRGBAAt(3, 0); c.G != 255 || c.R != 0 {
		t.Errorf("top-right = %+v, want green", c)
	}
	if c := dst.NRGBAAt(0, 3); c.B != 255 {
		t.Errorf("bottom-left = %+v, want blue", c)
	}
}

func TestResizeBilinearOnePixel(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	dst, err := resizeBilinear(context.Background(), src, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			if c := dst.NRGBAAt(x, y); c != (color.NRGBA{R: 10, G: 20, B: 30, A: 255}) {
				t.Fatalf("pixel %d,%d = %+v", x, y, c)
			}
		}
	}
}
