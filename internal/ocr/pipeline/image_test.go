package pipeline

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
)

// The wrapper exercises image.Image without the NRGBA fast path.
type genericImage struct{ image.Image }

func smallConfig() Config { c := DefaultConfig(); c.DetectorSide = 32; return c }

func TestScreenshotDetectionSizes(t *testing.T) {
	balanced := DefaultConfig()
	legacy := balanced
	legacy.DetectorSide, legacy.MaxDetectorPixels = 736, 4<<20
	bounded := balanced
	bounded.DetectorSide, bounded.DetectorLongSide, bounded.DetectorRoundUp = 64, 960, true
	for _, tc := range []struct {
		w, h                      int
		balanced, legacy, bounded [2]int
	}{
		{658, 137, [2]int{2144, 448}, [2]int{3520, 736}, [2]int{672, 160}},
		{800, 180, [2]int{1984, 448}, [2]int{3264, 736}, [2]int{800, 192}},
		{1920, 1080, [2]int{1344, 768}, [2]int{1920, 1088}, [2]int{960, 544}},
		{2048, 2048, [2]int{1024, 1024}, [2]int{2048, 2048}, [2]int{960, 960}},
		{225, 46, [2]int{2176, 448}, [2]int{3584, 736}, [2]int{320, 64}},
	} {
		for _, profile := range []struct {
			name string
			c    Config
			want [2]int
		}{
			{"balanced", balanced, tc.balanced}, {"legacy", legacy, tc.legacy}, {"bounded", bounded, tc.bounded},
		} {
			t.Run(fmt.Sprintf("%s/%dx%d", profile.name, tc.w, tc.h), func(t *testing.T) {
				w, h, err := detectorSize(tc.w, tc.h, profile.c)
				if err != nil || [2]int{w, h} != profile.want {
					t.Fatalf("got %dx%d %v want %v", w, h, err, profile.want)
				}
			})
		}
	}
}

func BenchmarkScreenshotPreprocessing(b *testing.B) {
	for _, size := range [][2]int{{658, 137}, {1920, 1080}} {
		for _, legacy := range []bool{false, true} {
			b.Run(fmt.Sprintf("%dx%d/legacy=%t", size[0], size[1], legacy), func(b *testing.B) {
				c := DefaultConfig()
				if legacy {
					c.DetectorSide, c.MaxDetectorPixels = 736, 4<<20
				}
				src, err := source(image.NewNRGBA(image.Rect(0, 0, size[0], size[1])), c)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, _, err := detectorInput(context.Background(), src, c); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestDetectorColorAndBounds(t *testing.T) {
	img := image.NewNRGBA(image.Rect(11, 17, 43, 49))
	for y := 17; y < 49; y++ {
		for x := 11; x < 43; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 128, B: 0, A: 255})
		}
	}
	for _, input := range []image.Image{img, genericImage{img}} {
		src, err := source(input, smallConfig())
		if err != nil {
			t.Fatal(err)
		}
		tensor, tr, err := detectorInput(context.Background(), src, smallConfig())
		if err != nil {
			t.Fatal(err)
		}
		if tr.padTop != 0 || tr.width != 32 || tr.height != 32 {
			t.Fatalf("transform: %+v", tr)
		}
		for k, want := range []float32{-1, 128.0/255*2 - 1, 1} {
			for _, offset := range []int{0, 511, 1023} {
				if math.Abs(float64(tensor.Data[k*1024+offset]-want)) > 1e-6 {
					t.Fatalf("BGR channel %d: %v", k, tensor.Data[k*1024+offset])
				}
			}
		}
	}
}

func TestAlphaAndBilinear(t *testing.T) {
	img := image.NewNRGBA(image.Rect(-2, -2, 0, 0))
	img.SetNRGBA(-2, -2, color.NRGBA{R: 255, A: 128})
	for _, input := range []image.Image{img, genericImage{img}} {
		s, err := source(input, smallConfig())
		if err != nil {
			t.Fatal(err)
		}
		p := s.at(0, 0)
		if math.Abs(p[0]-127) > .01 || p[2] != 255 {
			t.Fatalf("alpha: %v", p)
		}
		if s.at(1, 1) != [3]float64{255, 255, 255} {
			t.Fatal("transparent background")
		}
	}
	get := func(x, y int) [3]float64 { return [3]float64{float64(x*100 + y*20), 0, 0} }
	if got := bilinear(get, .25, .5)[0]; got != 35 {
		t.Fatalf("bilinear %v", got)
	}
}

func TestDetectorPaddingRoundingAndLimits(t *testing.T) {
	c := smallConfig()
	s, _ := source(image.NewNRGBA(image.Rect(0, 0, 160, 16)), c)
	_, tr, err := detectorInput(context.Background(), s, c)
	if err != nil {
		t.Fatal(err)
	}
	if tr.padTop != 22 || tr.paddedHeight != 60 || tr.width != 160 || tr.height != 64 {
		t.Fatalf("padding %+v", tr)
	}
	// Balanced mode retains the measured half-to-even alignment.
	s, _ = source(image.NewNRGBA(image.Rect(0, 0, 80, 80)), c)
	_, tr, err = detectorInput(context.Background(), s, c)
	if err != nil || tr.width != 64 {
		t.Fatalf("ties to even %+v %v", tr, err)
	}
	c.MaxDetectorPixels = 1024
	_, tr, err = detectorInput(context.Background(), s, c)
	if err != nil || tr.width != 32 || tr.height != 32 {
		t.Fatalf("minimum-sized detector: %+v %v", tr, err)
	}
	c.MaxDetectorPixels = 1023
	if _, _, err := detectorInput(context.Background(), s, c); !errors.Is(err, ErrLimit) {
		t.Fatalf("want limit: %v", err)
	}
	if _, err := source(image.NewNRGBA(image.Rect(0, 0, 0, 3)), c); err == nil {
		t.Fatal("empty image accepted")
	}
	c.MaxImagePixels = 100
	if _, err := source(image.NewNRGBA(image.Rect(0, 0, 11, 10)), c); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}

func TestDetectorSizeFitsBudgets(t *testing.T) {
	defaults := DefaultConfig()
	unaligned := defaults
	unaligned.MaxDetectorSide = 4095
	unaligned.MaxDetectorPixels = 2_000_003
	tensorLimited := defaults
	tensorLimited.MaxTensorElements = 3*1024 + 2
	for _, c := range []Config{defaults, unaligned, tensorLimited} {
		for _, size := range [][2]int{{600, 100}, {800, 100}, {1200, 200}, {3840, 2160}, {16384, 32}, {32, 16384}} {
			t.Run(fmt.Sprintf("%dx%d/side=%d/elements=%d", size[0], size[1], c.MaxDetectorSide, c.MaxTensorElements), func(t *testing.T) {
				w, h, err := detectorSize(size[0], size[1], c)
				if err != nil {
					t.Fatal(err)
				}
				if w < 32 || h < 32 || w%32 != 0 || h%32 != 0 || w > c.MaxDetectorSide || h > c.MaxDetectorSide || w*h > c.MaxDetectorPixels || 3*w*h > c.MaxTensorElements {
					t.Fatalf("detector %dx%d exceeds budgets %+v", w, h, c)
				}
				// Alignment may change each dimension by one 32-pixel cell.
				ratio := float64(size[0]) / float64(size[1])
				if math.Abs(float64(w)-float64(h)*ratio) > 32*(1+ratio) {
					t.Fatalf("aspect ratio lost: %v -> %dx%d", size, w, h)
				}
			})
		}
	}
	c := defaults
	c.MaxTensorElements = 3*32*32 - 1
	if _, _, err := detectorSize(600, 100, c); !errors.Is(err, ErrLimit) {
		t.Fatalf("undersized tensor budget: %v", err)
	}
}

func TestWideScreenshotPreprocessingKeepsFullImage(t *testing.T) {
	c := DefaultConfig()
	img := image.NewNRGBA(image.Rect(0, 0, 600, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 600; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / 599), A: 255})
		}
	}
	s, _ := source(img, c)
	tensor, tr, err := detectorInput(context.Background(), s, c)
	if err != nil {
		t.Fatal(err)
	}
	if tr.originalWidth != 600 || tr.originalHeight != 100 || tr.padTop != 0 || len(tensor.Data) != 3*tr.width*tr.height {
		t.Fatalf("transform %+v shape %v", tr, tensor.Shape)
	}
	red := tensor.Data[2*tr.width*tr.height:]
	if red[0] != -1 || red[tr.width-1] != 1 {
		t.Fatalf("cropped image edges: left=%v right=%v", red[0], red[tr.width-1])
	}
}

func TestRecognitionPaddingAndLongLine(t *testing.T) {
	c := DefaultConfig()
	img := crop{make([]byte, 3*16*8), 16, 8}
	for i := range img.data {
		img.data[i] = 255
	}
	tensor, err := recognitionInput(context.Background(), img, c)
	if err != nil {
		t.Fatal(err)
	}
	if tensor.Shape[3] != 320 {
		t.Fatal(tensor.Shape)
	}
	for k := 0; k < 3; k++ {
		for y := 0; y < 48; y++ {
			if tensor.Data[k*48*320+y*320+95] != 1 || tensor.Data[k*48*320+y*320+96] != 0 {
				t.Fatal("content or normalized zero padding")
			}
		}
	}
	img = crop{make([]byte, 3*1000), 1000, 1}
	if _, err := recognitionInput(context.Background(), img, c); !errors.Is(err, ErrLimit) {
		t.Fatalf("long line squeezed: %v", err)
	}
}

func TestPerspectiveCropAndRotation(t *testing.T) {
	img := image.NewNRGBA(image.Rect(20, 30, 40, 50))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.SetNRGBA(x+20, y+30, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), A: 255})
		}
	}
	s, _ := source(img, smallConfig())
	out, err := cropQuad(context.Background(), s, Quad{{2, 3}, {12, 3}, {12, 9}, {2, 9}}, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if out.w != 10 || out.h != 6 || out.at(0, 0) != [3]float64{0, 30, 20} || out.at(9, 5) != [3]float64{0, 80, 110} {
		t.Fatalf("crop dimensions or pixels %+v %v", out, out.at(9, 5))
	}
	rot, err := cropQuad(context.Background(), s, Quad{{2, 3}, {6, 3}, {6, 13}, {2, 13}}, smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	if rot.w != 10 || rot.h != 4 || rot.at(0, 0) != [3]float64{0, 30, 50} {
		t.Fatalf("rotation %dx%d %v", rot.w, rot.h, rot.at(0, 0))
	}
	// Counterclockwise: the source's right edge becomes the top row, and
	// its left edge becomes the bottom row. Check all four corners.
	if rot.at(9, 0) != [3]float64{0, 120, 50} || rot.at(0, 3) != [3]float64{0, 30, 20} || rot.at(9, 3) != [3]float64{0, 120, 20} {
		t.Fatal("crop rotation is not counterclockwise")
	}
	q := Quad{{2, 1}, {12, 3}, {10, 15}, {4, 11}}
	m, err := projective(q)
	if err != nil {
		t.Fatal(err)
	}
	for i, uv := range [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		den := m[6]*uv[0] + m[7]*uv[1] + 1
		p := Point{(m[0]*uv[0] + m[1]*uv[1] + m[2]) / den, (m[3]*uv[0] + m[4]*uv[1] + m[5]) / den}
		if distance(p, q[i]) > 1e-8 {
			t.Fatalf("corner %d: %v want %v", i, p, q[i])
		}
	}
	if _, err := projective(Quad{{0, 0}, {2, 2}, {0, 2}, {2, 0}}); err == nil {
		t.Fatal("crossed quad accepted")
	}
	c := smallConfig()
	c.MaxCropPixels = 10
	if _, err := cropQuad(context.Background(), s, q, c); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
