package capture

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestBitBltRejectsOversizedInput(t *testing.T) {
	for _, size := range [][2]int{{16384, 4097}, {16385, 1}} {
		img, err := bitBlt(0, 0, size[0], size[1], MaxSnapshotPixels)
		if img != nil || !errors.Is(err, ErrLimit) {
			t.Fatalf("oversized capture reached GDI: image=%v err=%v", img, err)
		}
	}
}

// TestBitBltSmoke exercises the production GDI core in physical pixels and keeps it
// as a PNG when GO_CAPTURE_OUT is set, for the manual checklist: run
//
//	CGO_ENABLED=0 SOTTO_CAPTURE_NATIVE=1 GO_CAPTURE_OUT=capture.png go test ./internal/capture/ -run '^TestBitBltSmoke$'
//
// and inspect the file — its corner must match the screen's top-left.
func TestBitBltSmoke(t *testing.T) {
	if os.Getenv("SOTTO_CAPTURE_NATIVE") != "1" {
		t.Skip("set SOTTO_CAPTURE_NATIVE=1 for desktop capture")
	}
	// The primary display contains the virtual desktop origin.
	monitor, _, err := modUser32.NewProc("MonitorFromPoint").Call(0, 1)
	if monitor == 0 {
		t.Fatalf("primary display: %v", err)
	}
	frames, err := SnapshotDisplays(context.Background(), []int64{int64(monitor)})
	if err != nil {
		t.Fatalf("bitBlt: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(frames))
	}
	img := frames[0].Image
	b := img.Bounds()
	if b.Empty() {
		t.Fatalf("unexpected physical capture bounds %+v", b)
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
