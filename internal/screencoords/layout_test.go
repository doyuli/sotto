package screencoords

import "testing"

func TestMixedDPILayoutConversions(t *testing.T) {
	// The 200% primary ends at DIP x=1920, while the 100% secondary
	// starts at x=3840. Selection bounds must use the physical owner screen.
	primary := Display{Pixels: Rect{0, 0, 3840, 2160}, DPI: 192}
	secondary := Display{Pixels: Rect{3840, 0, 1920, 2160}, DPI: 96}
	for _, displays := range [][]Display{{primary, secondary}, {secondary, primary}} {
		layout := NewLayout(displays, 96)
		for _, tt := range []struct {
			name  string
			input Rect
			want  Rect
		}{
			{"physical majority on secondary", Rect{3700, 100, 400, 100}, Rect{3700, 100, 400, 100}},
			{"physical majority on primary", Rect{3700, 100, 200, 100}, Rect{1850, 50, 100, 50}},
			{"physical point outside left", Rect{-100, 100, 1, 1}, Rect{-50, 50, 1, 1}},
			{"physical point outside right", Rect{6000, 100, 1, 1}, Rect{6000, 100, 1, 1}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				r := tt.input
				if got, ok := layout.PixelsToDIP(float64(r.X), float64(r.Y), float64(r.W), float64(r.H)); !ok || got != tt.want {
					t.Errorf("conversion(%+v) = %+v, %v; want %+v, true", tt.input, got, ok, tt.want)
				}
			})
		}
	}
}

func TestLayoutTiesAndEmptyScreens(t *testing.T) {
	primary := Display{Pixels: Rect{0, 0, 3840, 2160}, DPI: 192}
	secondary := Display{Pixels: Rect{3840, 0, 1920, 2160}, DPI: 96}
	// Equal physical overlap must use stable monitor order.
	for _, tt := range []struct {
		displays []Display
		want     Rect
	}{
		{[]Display{primary, secondary}, Rect{1895, 50, 50, 50}},
		{[]Display{secondary, primary}, Rect{3790, 100, 100, 100}},
	} {
		if got, ok := NewLayout(tt.displays, 96).PixelsToDIP(3790, 100, 100, 100); !ok || got != tt.want {
			t.Errorf("equal overlap = %+v, %v; want stable tie %+v", got, ok, tt.want)
		}
	}
	empty := NewLayout(nil, 144)
	if _, ok := empty.PixelsToDIP(0, 0, 100, 100); ok {
		t.Error("physical conversion without displays must report no screen")
	}
}

func TestLayoutSystemDPIFallback(t *testing.T) {
	for _, tt := range []struct {
		fallback int
		want     Rect
	}{
		{144, Rect{100, 100, 100, 100}},
		{0, Rect{150, 150, 150, 150}},
	} {
		layout := NewLayout([]Display{{Pixels: Rect{0, 0, 1920, 1080}}}, tt.fallback)
		got, ok := layout.PixelsToDIP(150, 150, 150, 150)
		if !ok || got != tt.want {
			t.Errorf("fallback %d = %+v, %v; want %+v", tt.fallback, got, ok, tt.want)
		}
	}
}

func TestFractionalUIABounds(t *testing.T) {
	layout := NewLayout([]Display{{Pixels: Rect{0, 0, 3840, 2160}, DPI: 192}}, 96)
	// UIA returns doubles. Rounding to whole pixels before scaling would
	// incorrectly move this rectangle to DIP (1, 1).
	got, ok := layout.PixelsToDIP(0.5, 0.5, 100.5, 40.5)
	if want := (Rect{0, 0, 50, 20}); !ok || got != want {
		t.Errorf("fractional UIA rectangle = %+v, %v; want %+v", got, ok, want)
	}
}
