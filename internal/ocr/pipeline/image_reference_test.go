package pipeline

import (
	"context"
	"image"
	"image/color"
	"math/rand/v2"
	"slices"
	"testing"
)

// Keep the scalar implementation as an independent oracle for tensor bytes.
func referenceDetectorInput(ctx context.Context, src pixels, c Config) (Tensor, detectorTransform, error) {
	pad := 0
	if src.h <= 30 || float64(src.w)/float64(src.h) > 8 {
		pad = (max(src.w/8, 30)*2 - src.h) / 2
	}
	ph := src.h + 2*pad
	w, h, err := detectorSize(src.w, ph, c)
	tr := detectorTransform{w, h, src.w, src.h, pad, ph}
	if err != nil {
		return Tensor{}, tr, err
	}
	shape := []int64{1, 3, int64(h), int64(w)}
	n, err := elements(shape, c.MaxTensorElements)
	if err != nil {
		return Tensor{}, tr, err
	}
	data := make([]float32, n)
	get := func(x, y int) [3]float64 {
		x, y = min(max(x, 0), src.w-1), min(max(y, 0), ph-1)
		if y < pad || y >= pad+src.h {
			return [3]float64{}
		}
		return src.at(x, y-pad)
	}
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return Tensor{}, tr, err
		}
		for x := 0; x < w; x++ {
			p := bilinear(get, (float64(x)+.5)*float64(src.w)/float64(w)-.5, (float64(y)+.5)*float64(ph)/float64(h)-.5)
			for channel := 0; channel < 3; channel++ {
				data[channel*w*h+y*w+x] = (float32(p[channel])/255 - .5) / .5
			}
		}
	}
	return Tensor{data, shape}, tr, nil
}

func TestDetectorRowCacheMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 7))
	for _, size := range [][2]int{{37, 19}, {83, 71}, {160, 16}} {
		img := image.NewNRGBA(image.Rect(11, -3, 11+size[0], -3+size[1]))
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				img.SetNRGBA(x, y, color.NRGBA{uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32())})
			}
		}
		for _, generic := range []bool{false, true} {
			for _, shrink := range []bool{false, true} {
				var input image.Image = img
				if generic {
					input = genericImage{img}
				}
				c := DefaultConfig()
				if shrink {
					c.DetectorSide, c.DetectorLongSide, c.DetectorRoundUp = 32, 64, true
				}
				src, err := source(input, c)
				if err != nil {
					t.Fatal(err)
				}
				got, tr, err := detectorInput(context.Background(), src, c)
				if err != nil {
					t.Fatal(err)
				}
				want, refTr, err := referenceDetectorInput(context.Background(), src, c)
				if err != nil {
					t.Fatal(err)
				}
				if tr != refTr || !slices.Equal(got.Data, want.Data) {
					t.Fatalf("tensor changed for %v generic=%t shrink=%t", size, generic, shrink)
				}
			}
		}
	}
}

func BenchmarkDetectorRowCache(b *testing.B) {
	c := DefaultConfig()
	src, _ := source(image.NewNRGBA(image.Rect(0, 0, 658, 137)), c)
	for _, cached := range []bool{false, true} {
		name := "scalar"
		run := referenceDetectorInput
		if cached {
			name = "cached"
			run = detectorInput
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := run(context.Background(), src, c); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
