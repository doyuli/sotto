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
	for value, want := range map[string]int{"": 2, "2": 2, " 4 ": 4, "1": 1, "16": 16} {
		if got, err := detectorThreads(value); err != nil || got != want {
			t.Fatalf("%q: %d %v", value, got, err)
		}
	}
	for _, value := range []string{"0", "-1", "17", "four"} {
		if _, err := detectorThreads(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
