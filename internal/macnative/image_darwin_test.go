//go:build darwin

package macnative

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"github.com/ebitengine/purego/objc"
)

func imageTestBridge(t testing.TB) (func(objc.ID, uintptr) uintptr, func(uintptr, uint64, uintptr) uintptr, func(uintptr)) {
	t.Helper()
	if os.Getenv("SOTTO_MACOS_NATIVE") != "1" {
		t.Skip("set SOTTO_MACOS_NATIVE=1 for native image conversion")
	}
	if _, err := Framework("Foundation"); err != nil {
		t.Fatal(err)
	}
	var source func(objc.ID, uintptr) uintptr
	var imageAt func(uintptr, uint64, uintptr) uintptr
	var release func(uintptr)
	if err := Bind("ImageIO", Symbol{&source, "CGImageSourceCreateWithData"}, Symbol{&imageAt, "CGImageSourceCreateImageAtIndex"}); err != nil {
		t.Fatal(err)
	}
	if err := Bind("CoreFoundation", Symbol{&release, "CFRelease"}); err != nil {
		t.Fatal(err)
	}
	return source, imageAt, release
}

func TestNativeImagePixelsAndLifetime(t *testing.T) {
	// PNG's decoder provides an independent orientation/alpha reference for the raw bridge.
	source, imageAt, release := imageTestBridge(t)
	input := image.NewNRGBA(image.Rect(4, 7, 11, 12))
	for y := 7; y < 12; y++ {
		for x := 4; x < 11; x++ {
			input.SetNRGBA(x, y, color.NRGBA{uint8(x * 20), uint8(y * 17), uint8(x + y), uint8(64 + (x+y)%4*63)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, input); err != nil {
		t.Fatal(err)
	}
	expected := image.NewRGBA(image.Rect(0, 0, 7, 5))
	draw.Draw(expected, expected.Rect, input, input.Rect.Min, draw.Src)
	var copied *image.RGBA
	Pool(func() {
		src := source(Data(encoded.Bytes()), 0)
		if src == 0 {
			t.Fatal("create PNG source")
		}
		defer release(src)
		cg := imageAt(src, 0, 0)
		if cg == 0 {
			t.Fatal("decode native PNG")
		}
		defer release(cg)
		var err error
		copied, err = CopyNativeImage(cg)
		if err != nil {
			t.Fatal(err)
		}
	})
	comparePixels(t, copied, expected)
	// Use a subimage with a nonzero origin and a stride wider than its bounds.
	crop := input.SubImage(image.Rect(5, 8, 10, 11))
	expected = image.NewRGBA(image.Rect(0, 0, 5, 3))
	draw.Draw(expected, expected.Rect, crop, crop.Bounds().Min, draw.Src)
	for range 20 {
		err := WithNativeImage(crop, func(cg uintptr) error {
			var err error
			copied, err = CopyNativeImage(cg)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		comparePixels(t, copied, expected)
	}
	if err := WithNativeImage(nil, func(uintptr) error { t.Fatal("accepted nil image"); return nil }); err == nil {
		t.Fatal("missing image error")
	}
	sentinel := fmt.Errorf("consumer failure")
	if err := WithNativeImage(input, func(uintptr) error { return sentinel }); err != sentinel {
		t.Fatal(err)
	}
}

func comparePixels(t *testing.T, got, want *image.RGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("bounds %v != %v", got.Bounds(), want.Bounds())
	}
	for i, b := range got.Pix {
		d := int(b) - int(want.Pix[i])
		if d < -1 || d > 1 {
			t.Fatalf("pixel byte %d: %d != %d", i, b, want.Pix[i])
		}
	}
}

func BenchmarkVisionInputConversion(b *testing.B) {
	imageTestBridge(b)
	input := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			input.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	b.Run("png", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var encoded bytes.Buffer
			encoder := png.Encoder{CompressionLevel: png.BestSpeed}
			if err := encoder.Encode(&encoded, input); err != nil {
				b.Fatal(err)
			}
			Pool(func() {
				if Data(encoded.Bytes()) == 0 {
					b.Fatal("native data")
				}
			})
		}
	})
	b.Run("pixels", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := WithNativeImage(input, func(uintptr) error { return nil }); err != nil {
				b.Fatal(err)
			}
		}
	})
}
