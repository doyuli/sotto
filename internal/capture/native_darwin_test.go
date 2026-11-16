//go:build darwin

package capture

import (
	"context"
	"os"
	"sotto/internal/macnative"
	"testing"
	"time"
)

func TestMacCaptureSymbols(t *testing.T) {
	if err := loadCapture(); err != nil {
		t.Fatal(err)
	}
}

func TestMacCaptureNative(t *testing.T) {
	if os.Getenv("SOTTO_MACOS_NATIVE") != "1" {
		t.Skip("set SOTTO_MACOS_NATIVE=1 with screen recording permission")
	}
	if err := loadCapture(); err != nil {
		t.Fatal(err)
	}
	if err := macnative.RequireScreenRecording(false); err != nil {
		t.Fatal(err)
	}
	var mainDisplay func() uint32
	if err := macnative.Bind("CoreGraphics", macnative.Symbol{Target: &mainDisplay, Name: "CGMainDisplayID"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	frames, err := SnapshotDisplays(ctx, []int64{int64(mainDisplay())})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(frames))
	}
	img := frames[0].Image
	if img.Bounds().Dx() < 80 || img.Bounds().Dy() < 40 {
		t.Fatalf("dimensions=%v", img.Bounds())
	}
}
