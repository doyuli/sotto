package ocr

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"

	"sotto/internal/ocr/pipeline"
)

const maxOCRDimension = 3800

func fitSystemImage(ctx context.Context, img image.Image, limit int) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= limit && h <= limit {
		return img, nil
	}
	ratio := min(float64(limit)/float64(w), float64(limit)/float64(h))
	return resizeBilinear(ctx, img, max(1, int(float64(w)*ratio)), max(1, int(float64(h)*ratio)))
}

type systemLine struct {
	Text string     `json:"text"`
	Box  [4]float64 `json:"box"`
}

func systemLines(lines []systemLine, original, processed image.Rectangle) ([]pipeline.Line, error) {
	if len(lines) > 2000 {
		return nil, fmt.Errorf("%w: system OCR lines", pipeline.ErrLimit)
	}
	if original.Empty() || processed.Empty() {
		return nil, fmt.Errorf("ocr: empty system image")
	}
	sx, sy := float64(original.Dx())/float64(processed.Dx()), float64(original.Dy())/float64(processed.Dy())
	result := make([]pipeline.Line, 0, len(lines))
	for _, line := range lines {
		for _, v := range line.Box {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return nil, fmt.Errorf("ocr: invalid system box")
			}
		}
		x, y, w, h := line.Box[0], line.Box[1], line.Box[2], line.Box[3]
		right, bottom := min(float64(processed.Dx()), x+w), min(float64(processed.Dy()), y+h)
		if right <= x || bottom <= y {
			return nil, fmt.Errorf("ocr: empty system box")
		}
		result = append(result, pipeline.Line{Text: line.Text, Quad: pipeline.Quad{{X: x * sx, Y: y * sy}, {X: right * sx, Y: y * sy}, {X: right * sx, Y: bottom * sy}, {X: x * sx, Y: bottom * sy}}})
	}
	return result, nil
}

// resizeBilinear scales img to w×h with bilinear interpolation. The
// recognizer does better with smooth edges than with the stair-steps of
// nearest-neighbour, and a hand-rolled scaler keeps the dependency list
// as it is.
func resizeBilinear(ctx context.Context, img image.Image, w, h int) (*image.NRGBA, error) {
	src := img.Bounds()
	sw, sh := src.Dx(), src.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if sw == 0 || sh == 0 {
		return dst, nil
	}
	// Screen captures arrive as *image.NRGBA; reading its pixels directly
	// keeps a 3× upscale in the low milliseconds instead of hundreds.
	nrgba, direct := img.(*image.NRGBA)
	at := func(x, y int) color.NRGBA {
		if direct {
			i := nrgba.PixOffset(x, y)
			return color.NRGBA{nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2], nrgba.Pix[i+3]}
		}
		return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	}
	// Sample at pixel centers: dst pixel i maps to (i+0.5)*sw/w - 0.5.
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sy := (float64(y)+0.5)*float64(sh)/float64(h) - 0.5
		y0 := int(sy)
		if sy < 0 {
			y0 = 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		fy := sy - float64(y0)
		if fy < 0 {
			fy = 0
		}
		for x := 0; x < w; x++ {
			sx := (float64(x)+0.5)*float64(sw)/float64(w) - 0.5
			x0 := int(sx)
			if sx < 0 {
				x0 = 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			fx := sx - float64(x0)
			if fx < 0 {
				fx = 0
			}
			c00 := at(src.Min.X+x0, src.Min.Y+y0)
			c10 := at(src.Min.X+x1, src.Min.Y+y0)
			c01 := at(src.Min.X+x0, src.Min.Y+y1)
			c11 := at(src.Min.X+x1, src.Min.Y+y1)
			dst.SetNRGBA(x, y, color.NRGBA{
				R: bilerp(c00.R, c10.R, c01.R, c11.R, fx, fy),
				G: bilerp(c00.G, c10.G, c01.G, c11.G, fx, fy),
				B: bilerp(c00.B, c10.B, c01.B, c11.B, fx, fy),
				A: bilerp(c00.A, c10.A, c01.A, c11.A, fx, fy),
			})
		}
	}
	return dst, nil
}

// bilerp weighs the four neighbours at (fx, fy).
func bilerp(c00, c10, c01, c11 uint8, fx, fy float64) uint8 {
	top := float64(c00)*(1-fx) + float64(c10)*fx
	bottom := float64(c01)*(1-fx) + float64(c11)*fx
	v := top*(1-fy) + bottom*fy
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}
