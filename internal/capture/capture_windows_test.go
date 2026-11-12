package capture

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestBitBltRejectsOversizedInput(t *testing.T) {
	for _, size := range [][2]int{{7680, 4320}, {16385, 1}} {
		img, err := bitBlt(0, 0, size[0], size[1])
		if img != nil || !errors.Is(err, ErrLimit) {
			t.Fatalf("oversized capture reached GDI: image=%v err=%v", img, err)
		}
	}
}

// TestGrabSmoke captures a small region of the real screen and keeps it
// as a PNG when GO_CAPTURE_OUT is set, for the manual checklist: run
//
//	GO_CAPTURE_OUT=capture.png go test ./internal/capture/
//
// and inspect the file — its corner must match the screen's top-left.
func TestGrabSmoke(t *testing.T) {
	img, err := Grab(Rect{X: 0, Y: 0, W: 320, H: 200})
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		t.Fatalf("empty capture %+v", b)
	}
	if out := os.Getenv("GO_CAPTURE_OUT"); out != "" {
		f, err := os.Create(filepath.Clean(out))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", out)
	}
}
