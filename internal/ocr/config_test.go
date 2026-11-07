package ocr

import (
	"reflect"
	"testing"

	"sotto/internal/ocr/pipeline"
)

func TestDetectorProfiles(t *testing.T) {
	for _, tc := range []struct {
		profile             string
		short, long, pixels int
		up                  bool
	}{
		{"", 448, 4096, 1 << 20, false},
		{"balanced", 448, 4096, 1 << 20, false},
		{"bounded", 64, 960, 1 << 20, true},
		{"legacy", 736, 4096, 4 << 20, false},
	} {
		c, err := detectorConfig(tc.profile)
		if err != nil || c.DetectorSide != tc.short || c.DetectorLongSide != tc.long || c.MaxDetectorPixels != tc.pixels || c.DetectorRoundUp != tc.up {
			t.Fatalf("%s: %+v %v", tc.profile, c, err)
		}
		// Profile selection must not silently change recognition or filtering.
		c.DetectorSide, c.DetectorLongSide, c.MaxDetectorPixels, c.DetectorRoundUp = 448, 4096, 1<<20, false
		if !reflect.DeepEqual(c, pipeline.DefaultConfig()) {
			t.Fatalf("profile changes unrelated settings: %+v", c)
		}
	}
	if _, err := detectorConfig("typo"); err == nil {
		t.Fatal("accepted unknown profile")
	}
}

func TestDetectorThreads(t *testing.T) {
	for cpus, want := range map[int]int{2: 2, 8: 4} {
		if got, err := detectorThreads("", cpus); err != nil || got != want {
			t.Fatalf("default with %d CPUs: got %d, want %d, err=%v", cpus, got, want, err)
		}
	}
	// Explicit overrides support comparisons even on a low-core machine.
	for value, want := range map[string]int{" 4 ": 4, "16": 16} {
		if got, err := detectorThreads(value, 1); err != nil || got != want {
			t.Fatalf("%q: %d %v", value, got, err)
		}
	}
	for _, value := range []string{"0", "17", "four"} {
		if _, err := detectorThreads(value, 8); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
