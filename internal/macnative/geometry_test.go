package macnative

import (
	"math"
	"testing"
)

func TestCocoaRectOnSecondaryDisplays(t *testing.T) {
	for _, r := range []Rect{NewRect(-1920, 100, 640, 480), NewRect(0, 1200, 400, 200), NewRect(300, -400, 300, 200)} {
		if got := CocoaRect(CocoaRect(r, 900), 900); got != r {
			t.Fatalf("flip round trip: %v != %v", got, r)
		}
	}
	if r := CocoaRect(NewRect(-1920, 100, 640, 480), 900); r.Origin.X != -1920 || r.Origin.Y != 320 {
		t.Fatalf("secondary: %+v", r)
	}
}

func TestCapturePhysicalBudget(t *testing.T) {
	w, h, err := CaptureDimensions(NewRect(10.5, 20.5, 100, 60), 2, 16384, 16<<20)
	if err != nil || w != 200 || h != 120 {
		t.Fatalf("Retina: %d x %d: %v", w, h, err)
	}
	for _, r := range []Rect{NewRect(-1, 0, 100, 100), NewRect(0, 0, 10000, 10000), NewRect(math.NaN(), 0, 10, 10), NewRect(0, 0, 0, 100)} {
		if _, _, err := CaptureDimensions(r, 2, 16384, 16<<20); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}
