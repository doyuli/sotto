package screencoords

import "testing"

func TestDisplayCoordinateArithmetic(t *testing.T) {
	// These cases cover arithmetic within a known display. Layout tests
	// separately exercise monitor choice before performing the conversion.
	for _, tt := range []struct {
		name                  string
		origin, dpi, minDPI   int
		pixel                 float64
		wantOrigin, wantPoint int
	}{
		{"primary 100%", 0, 96, 96, 150, 0, 150},
		{"right 125%", 1920, 120, 96, 2045, 1920, 2020},
		{"right 150%", 1920, 144, 96, 2070, 1920, 2020},
		{"right 200%", 1920, 192, 96, 2120, 1920, 2020},
		{"left 150%", -2560, 144, 96, -2410, -2560, -2460},
		{"above 200%", -2160, 192, 96, -1960, -2160, -2060},
		{"uniform 150%", -1920, 144, 144, -1770, -1280, -1180},
		{"minimum 125%", 1920, 144, 120, 2070, 1536, 1636},
	} {
		t.Run(tt.name, func(t *testing.T) {
			origin := Origin(tt.origin, tt.minDPI)
			if origin != tt.wantOrigin {
				t.Fatalf("display origin = %d, want %d", origin, tt.wantOrigin)
			}
			point := ToDIP(tt.pixel, tt.origin, origin, tt.dpi)
			if point != tt.wantPoint {
				t.Fatalf("screen coordinate = %d, want %d", point, tt.wantPoint)
			}
			if pixel := ToPixels(point, tt.origin, origin, tt.dpi); pixel != int(tt.pixel) {
				t.Errorf("capture pixel = %d, want %v", pixel, tt.pixel)
			}
		})
	}
}

func TestScaleRounding(t *testing.T) {
	for _, tt := range []struct {
		value float64
		dpi   int
		want  int
	}{
		{0, 96, 0},
		{100, 96, 100},
		{150, 144, 100},
		{96, 120, 77},
		{-96, 120, -77},
		{-1920, 144, -1280},
		{0.75, 144, 1},
		{-0.75, 144, 0},
		{-1.5, 96, -1},
	} {
		if got := Scale(tt.value, 96, tt.dpi); got != tt.want {
			t.Errorf("Scale(%v, 96, %d) = %d, want %d", tt.value, tt.dpi, got, tt.want)
		}
	}
}
