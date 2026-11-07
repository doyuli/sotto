package pipeline

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"testing"
)

func probabilityMap(w, h int, fill func(int, int) float32) Tensor {
	t := Tensor{make([]float32, w*h), []int64{1, 1, int64(h), int64(w)}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t.Data[y*w+x] = fill(x, y)
		}
	}
	return t
}
func transform(w, h int) detectorTransform { return detectorTransform{w, h, w, h, 0, h} }

func TestDetectSeparateRotatedAndNoise(t *testing.T) {
	c := smallConfig()
	c.Dilation = false
	tensor := probabilityMap(100, 80, func(x, y int) float32 {
		if x >= 10 && x <= 30 && y >= 10 && y <= 18 {
			return .9
		}
		if x >= 60 && x <= 85 && math.Abs(float64(y)-(.4*float64(x)+10)) <= 4 {
			return .95
		}
		if x == 2 && y == 70 {
			return .99
		}
		return 0
	})
	boxes, stats, err := detect(context.Background(), tensor, transform(100, 80), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 2 || stats.SmallRegions != 1 {
		t.Fatalf("boxes %v stats %+v", boxes, stats)
	}
	if boxes[0].quad[0] != (Point{5, 5}) || boxes[0].quad[2] != (Point{35, 23}) {
		t.Fatalf("analytic unclip: %v", boxes[0].quad)
	}
	if math.Abs(boxes[1].quad[1].Y-boxes[1].quad[0].Y) < 5 {
		t.Fatalf("lost rotated geometry: %v", boxes[1].quad)
	}
}

func TestMinimumRectAgainstExhaustiveProjection(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 9))
	for trial := 0; trial < 100; trial++ {
		points := make([]Point, 80)
		for i := range points {
			points[i] = Point{rng.Float64() * 100, rng.Float64() * 40}
		}
		hull := convexHull(points)
		q, _ := minimumRect(hull)
		want := math.Inf(1)
		// Independent O(n²) projection oracle for the linear caliper code.
		for i, a := range hull {
			b := hull[(i+1)%len(hull)]
			d := distance(a, b)
			u, v := Point{(b.X - a.X) / d, (b.Y - a.Y) / d}, Point{-(b.Y - a.Y) / d, (b.X - a.X) / d}
			loU, hiU, loV, hiV := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
			for _, p := range hull {
				du, dv := dot(p, u), dot(p, v)
				loU = min(loU, du)
				hiU = max(hiU, du)
				loV = min(loV, dv)
				hiV = max(hiV, dv)
			}
			want = min(want, (hiU-loU)*(hiV-loV))
		}
		got := distance(q[0], q[1]) * distance(q[0], q[3])
		if math.Abs(got-want) > 1e-6 {
			t.Fatalf("trial %d area %.9f want %.9f", trial, got, want)
		}
		for _, p := range hull {
			if !inside(q, p) {
				t.Fatalf("hull outside rectangle: %v %v", q, p)
			}
		}
	}
}

func TestDetectDilationHolesAndMapping(t *testing.T) {
	c := smallConfig()
	tensor := probabilityMap(64, 64, func(x, y int) float32 {
		if x >= 10 && x <= 30 && y >= 20 && y <= 30 {
			return .95
		}
		return 0
	})
	tr := detectorTransform{64, 64, 128, 96, 16, 128}
	boxes, _, err := detect(context.Background(), tensor, tr, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes) != 1 {
		t.Fatal(boxes)
	}
	// Dilation adds one pixel to right/bottom, then scale 2 and subtract pad.
	if boxes[0].quad[0] != (Point{8, 12}) || boxes[0].quad[2] != (Point{74, 58}) {
		t.Fatalf("mapped: %v", boxes[0].quad)
	}
	c.Dilation = false
	c.BoxThreshold = .1
	ring := probabilityMap(40, 40, func(x, y int) float32 {
		if x >= 8 && x <= 30 && y >= 8 && y <= 30 && (x < 12 || x > 26 || y < 12 || y > 26) {
			return .9
		}
		return 0
	})
	boxes, stats, err := detect(context.Background(), ring, transform(40, 40), c)
	if err != nil || len(boxes) != 1 || stats.Components != 1 {
		t.Fatalf("hole must not duplicate text: %v %+v %v", boxes, stats, err)
	}
}

func TestDetectFailuresAndBudgets(t *testing.T) {
	tensor := probabilityMap(64, 64, func(x, y int) float32 {
		if x > 8 && x < 40 && y > 8 && y < 24 {
			return .4
		}
		return 0
	})
	c := smallConfig()
	c.Dilation = false
	boxes, stats, err := detect(context.Background(), tensor, transform(64, 64), c)
	if err != nil || len(boxes) != 0 || stats.LowScoreRegions != 1 {
		t.Fatalf("low score %+v %v", stats, err)
	}
	c.BoxThreshold = .3
	c.MaxScorePixels = 10
	if _, _, err := detect(context.Background(), tensor, transform(64, 64), c); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	c = smallConfig()
	c.MaxBoundaryPoints = 3
	if _, _, err := detect(context.Background(), tensor, transform(64, 64), c); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	c = smallConfig()
	c.Dilation = false
	c.MaxCandidates = 1
	noise := probabilityMap(8, 8, func(x, y int) float32 {
		if (x == 0 && y == 0) || (x == 7 && y == 7) {
			return 1
		}
		return 0
	})
	if _, _, err := detect(context.Background(), noise, transform(8, 8), c); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	for _, bad := range []Tensor{{nil, []int64{1, 1, 2, 2}}, {[]float32{float32(math.NaN())}, []int64{1, 1, 1, 1}}, {[]float32{2}, []int64{1, 1, 1, 1}}, {[]float32{0}, []int64{1, 1, -1, 1}}, {[]float32{0}, []int64{1, 1, 1 << 50, 1}}} {
		if _, _, err := detect(context.Background(), bad, transform(8, 8), smallConfig()); err == nil {
			t.Fatalf("accepted invalid tensor %+v", bad)
		}
	}
}
