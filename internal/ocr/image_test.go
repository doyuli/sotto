package ocr

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestFitDimension(t *testing.T) {
	tests := []struct {
		w, h         int
		wantW, wantH int
	}{
		{1920, 1080, 1920, 1080},
		{5120, 2880, 3800, 2137},
		{3800, 100, 3800, 100},
	}
	for _, tt := range tests {
		got := fitDimension(image.NewNRGBA(image.Rect(0, 0, tt.w, tt.h)))
		if b := got.Bounds(); b.Dx() != tt.wantW || b.Dy() != tt.wantH {
			t.Errorf("fitDimension(%dx%d) = %dx%d, want %dx%d", tt.w, tt.h, b.Dx(), b.Dy(), tt.wantW, tt.wantH)
		}
	}
}

func TestRetryScale(t *testing.T) {
	lines := func(h int) []Line {
		return []Line{{Box: [4]int{0, 0, 100, h}}}
	}
	tests := []struct {
		name string
		ls   []Line
		w, h int
		want float64
	}{
		{"empty means blind retry", nil, 500, 52, 2},
		{"small text aims at 40px", lines(18), 500, 52, 40.0 / 18.0},
		{"large text needs no retry", lines(40), 500, 52, 1},
		{"huge text needs no retry", lines(60), 500, 52, 1},
		{"tiny text is capped at 4x", lines(5), 500, 52, 4},
		{"image over the engine limit", lines(18), 4000, 3000, 1},
		{"marginal gain is skipped", lines(33), 500, 52, 1},
	}
	for _, tt := range tests {
		got := retryScale(tt.ls, tt.w, tt.h, maxOCRDimension)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("%s: retryScale = %.3f, want %.3f", tt.name, got, tt.want)
		}
	}
}

func TestScaledSize(t *testing.T) {
	w, h := scaledSize(500, 52, 2.22)
	if w < 500 || h < 52 {
		t.Fatalf("scaled size shrank: %dx%d", w, h)
	}
	if w != 1110 && w != 1111 {
		t.Errorf("width = %d, want about 1110", w)
	}
}

func TestMapLines(t *testing.T) {
	ls := []Line{{Box: [4]int{100, 50, 200, 40}}}
	mapLines(ls, 2)
	if ls[0].Box != [4]int{50, 25, 100, 20} {
		t.Errorf("mapped box = %v", ls[0].Box)
	}
}

func TestMedianLineHeight(t *testing.T) {
	ls := []Line{{Box: [4]int{0, 0, 1, 10}}, {Box: [4]int{0, 0, 1, 20}}, {Box: [4]int{0, 0, 1, 30}}}
	if got := medianLineHeight(ls); got != 20 {
		t.Errorf("median = %v, want 20", got)
	}
	if got := medianLineHeight(nil); got != 0 {
		t.Errorf("empty median = %v, want 0", got)
	}
}

func TestOrderLines(t *testing.T) {
	// Two columns on one visual row, then a line below.
	ls := []Line{
		{Text: "right", Box: [4]int{300, 10, 100, 20}},
		{Text: "below", Box: [4]int{10, 40, 100, 20}},
		{Text: "left", Box: [4]int{10, 12, 100, 20}},
	}
	got := orderLines(ls)
	if got[0].Text != "left" || got[1].Text != "right" || got[2].Text != "below" {
		t.Errorf("order = %q, want left, right, below", []string{got[0].Text, got[1].Text, got[2].Text})
	}
}

func TestCountNonSpace(t *testing.T) {
	if got := countNonSpace(" a b\n中 文 "); got != 4 {
		t.Errorf("countNonSpace = %d, want 4", got)
	}
}

func TestResizeBilinearKeepsCorners(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	src.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	src.SetNRGBA(1, 1, color.NRGBA{A: 255})
	dst := resizeBilinear(src, 4, 4)
	if b := dst.Bounds(); b.Dx() != 4 || b.Dy() != 4 {
		t.Fatalf("size = %v, want 4x4", b)
	}
	// The corners take their source pixel's colour.
	if c := dst.NRGBAAt(0, 0); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("top-left = %+v, want red", c)
	}
	if c := dst.NRGBAAt(3, 0); c.G != 255 || c.R != 0 {
		t.Errorf("top-right = %+v, want green", c)
	}
	if c := dst.NRGBAAt(0, 3); c.B != 255 {
		t.Errorf("bottom-left = %+v, want blue", c)
	}
}

func TestResizeBilinearOnePixel(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	dst := resizeBilinear(src, 3, 3)
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			if c := dst.NRGBAAt(x, y); c != (color.NRGBA{R: 10, G: 20, B: 30, A: 255}) {
				t.Fatalf("pixel %d,%d = %+v", x, y, c)
			}
		}
	}
}
