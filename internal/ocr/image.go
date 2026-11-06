package ocr

import (
	"image"
	"image/color"
	"math"
	"sort"
	"unicode"
)

// Windows' recognizer reads a screenshot of UI text poorly at its native
// size: a 12-16px line loses half its characters. Enlarging a small
// selection two or three times is the single biggest accuracy win, and
// the cost is a few milliseconds. A very large shot is scaled down
// instead — OcrEngine.MaxImageDimension rejects anything beyond it.

const (
	// maxOCRDimension stays under OcrEngine's own limit (4096 on the
	// engines shipped with Windows 10/11) with some room to spare.
	maxOCRDimension = 3800
	// Line height, in pixels, a retry pass aims for. Windows' recognizer
	// reads glyphs reliably at roughly this size but drops characters
	// from a 12–16 px laptop screenshot.
	targetLineHeight = 40.0
	// Lines at least this tall are good enough that a retry would only
	// cost time.
	acceptableLineHeight = 26.0
	// Scale used when the first pass found no line at all, so there is no
	// height to measure.
	blindRetryScale = 2.0
	// A smaller enlargement cannot change the outcome enough to pay for a
	// second pass.
	minUsefulScale = 1.25
	maxRetryScale  = 4.0
	// Upper bound on the retry bitmap, keeping the extra buffer around
	// 64 MB.
	maxScaledPixels = 16_000_000
)

// fitDimension shrinks an image that exceeds the recognizer's limit. It
// never enlarges: the retry pass below does that only when the first
// pass proved the text is too small.
func fitDimension(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || (w <= maxOCRDimension && h <= maxOCRDimension) {
		return img
	}
	rx := float64(maxOCRDimension) / float64(w)
	ry := float64(maxOCRDimension) / float64(h)
	r := rx
	if ry < r {
		r = ry
	}
	tw, th := int(float64(w)*r), int(float64(h)*r)
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	return resizeBilinear(img, tw, th)
}

// retryScale decides how much to enlarge the capture for a second
// recognition pass: aim for line height 40 px, blind 2× when the first
// pass found nothing, and never exceed the engine's dimension limit or
// the memory budget.
func retryScale(lines []Line, w, h, maxDim int) float64 {
	if w <= 0 || h <= 0 || maxDim <= 0 {
		return 1
	}
	if w > maxDim || h > maxDim {
		return 1
	}
	median := medianLineHeight(lines)
	var desired float64
	switch {
	case median <= 0:
		desired = blindRetryScale
	case median >= acceptableLineHeight:
		return 1
	default:
		desired = targetLineHeight / median
	}
	if desired > maxRetryScale {
		desired = maxRetryScale
	}
	limitDim := math.Min(float64(maxDim)/float64(w), float64(maxDim)/float64(h))
	limitPixels := math.Sqrt(float64(maxScaledPixels) / (float64(w) * float64(h)))
	if limitDim < desired {
		desired = limitDim
	}
	if limitPixels < desired {
		desired = limitPixels
	}
	if desired < minUsefulScale {
		return 1
	}
	return desired
}

// medianLineHeight is the median of the recognized lines' box heights,
// ignoring degenerate entries.
func medianLineHeight(lines []Line) float64 {
	heights := make([]float64, 0, len(lines))
	for _, l := range lines {
		if l.Box[3] > 0 {
			heights = append(heights, float64(l.Box[3]))
		}
	}
	if len(heights) == 0 {
		return 0
	}
	sort.Float64s(heights)
	mid := len(heights) / 2
	if len(heights)%2 == 1 {
		return heights[mid]
	}
	return (heights[mid-1] + heights[mid]) / 2
}

// scaledSize rounds a scaled image size to whole pixels, never smaller
// than the source.
func scaledSize(w, h int, scale float64) (int, int) {
	tw := int(math.Round(float64(w) * scale))
	th := int(math.Round(float64(h) * scale))
	if tw < w {
		tw = w
	}
	if th < h {
		th = h
	}
	return tw, th
}

// mapLines maps boxes recognized on an enlarged image back onto the
// source image's coordinates.
func mapLines(lines []Line, scale float64) {
	if scale <= 0 || scale == 1 {
		return
	}
	for i := range lines {
		for j := range lines[i].Box {
			lines[i].Box[j] = int(math.Round(float64(lines[i].Box[j]) / scale))
		}
	}
}

// orderLines re-orders recognized lines into reading order: rows by
// vertical position (with a tolerance of half the average line height,
// so columns on one visual row stay together), left to right inside a
// row.
func orderLines(lines []Line) []Line {
	if len(lines) <= 1 {
		return lines
	}
	var sum float64
	var count int
	for _, l := range lines {
		if l.Box[3] > 0 {
			sum += float64(l.Box[3])
			count++
		}
	}
	avg := 20.0
	if count > 0 {
		avg = sum / float64(count)
	}
	tolerance := avg * 0.5

	out := append([]Line(nil), lines...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Box[1] < out[j].Box[1] })

	var rows [][]Line
	if len(out) > 0 {
		row := []Line{out[0]}
		rowSum := out[0].Box[1]
		for _, l := range out[1:] {
			avgY := rowSum / len(row)
			if math.Abs(float64(l.Box[1]-avgY)) <= tolerance {
				row = append(row, l)
				rowSum += l.Box[1]
			} else {
				rows = append(rows, row)
				row = []Line{l}
				rowSum = l.Box[1]
			}
		}
		rows = append(rows, row)
	}

	result := make([]Line, 0, len(out))
	for _, row := range rows {
		sort.SliceStable(row, func(i, j int) bool { return row[i].Box[0] < row[j].Box[0] })
		result = append(result, row...)
	}
	return result
}

// countNonSpace counts the non-whitespace characters a recognition
// produced; the pass that recovered more of them is the better reading.
func countNonSpace(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// resizeBilinear scales img to w×h with bilinear interpolation. The
// recognizer does better with smooth edges than with the stair-steps of
// nearest-neighbour, and a hand-rolled scaler keeps the dependency list
// as it is.
func resizeBilinear(img image.Image, w, h int) *image.NRGBA {
	src := img.Bounds()
	sw, sh := src.Dx(), src.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if sw == 0 || sh == 0 {
		return dst
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
	return dst
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
