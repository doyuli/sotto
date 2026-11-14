//go:build darwin

package ocr

import (
	"context"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVisionNativeSmoke(t *testing.T) {
	if os.Getenv("SOTTO_MACOS_NATIVE") != "1" {
		t.Skip("set SOTTO_MACOS_NATIVE=1 to run native Vision recognition")
	}
	f, err := os.Open("testdata/bilingual.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Recognize(ctx, img)
	if err != nil {
		t.Fatal(err)
	}
	if got.Engine != "vision" || got.Text == "" || len(got.Lines) == 0 {
		t.Fatalf("empty native result: engine=%s lines=%d", got.Engine, len(got.Lines))
	}
	for _, word := range []string{"DeepL", "翻译"} {
		if !strings.Contains(got.Text, word) {
			t.Fatalf("missing %q in recognized text", word)
		}
	}
	for _, line := range got.Lines {
		for _, point := range line.Quad {
			if point.X < 0 || point.Y < 0 || point.X > float64(img.Bounds().Dx()) || point.Y > float64(img.Bounds().Dy()) {
				t.Fatal("observation outside image")
			}
		}
	}
	t.Logf("Vision recognized %d lines and %d text bytes", len(got.Lines), len(got.Text))
}
