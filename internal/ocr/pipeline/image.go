package pipeline

import (
	"context"
	"fmt"
	"image"
	"math"
)

type pixels struct {
	image image.Image
	nrgba *image.NRGBA
	w, h  int
	min   image.Point
}

func source(img image.Image, c Config) (pixels, error) {
	if img == nil {
		return pixels{}, fmt.Errorf("empty image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return pixels{}, fmt.Errorf("empty image")
	}
	if w > c.MaxImageSide || h > c.MaxImageSide || w > c.MaxImagePixels/h {
		return pixels{}, fmt.Errorf("%w: image %dx%d", ErrLimit, w, h)
	}
	nrgba, _ := img.(*image.NRGBA)
	return pixels{img, nrgba, w, h, b.Min}, nil
}

// Screenshots are opaque. Transparent inputs are deterministically composited
// onto white to avoid content-dependent background changes.
func (s pixels) at(x, y int) [3]float64 {
	x, y = min(max(x, 0), s.w-1), min(max(y, 0), s.h-1)
	if s.nrgba != nil {
		i := s.nrgba.PixOffset(x+s.min.X, y+s.min.Y)
		p := s.nrgba.Pix[i : i+4]
		a := float64(p[3]) / 255
		return [3]float64{float64(p[2])*a + 255*(1-a), float64(p[1])*a + 255*(1-a), float64(p[0])*a + 255*(1-a)}
	}
	r, g, b, a := s.image.At(x+s.min.X, y+s.min.Y).RGBA()
	return [3]float64{float64(b+65535-a) / 257, float64(g+65535-a) / 257, float64(r+65535-a) / 257}
}

type detectorTransform struct {
	width, height                 int // Actual tensor dimensions, including vertical padding.
	originalWidth, originalHeight int
	padTop, paddedHeight          int
}

func detectorInput(ctx context.Context, src pixels, c Config) (Tensor, detectorTransform, error) {
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
	// Reuse horizontal interpolation for adjacent output rows. Enlargement
	// often samples the same source rows many times; do the pixel conversion
	// and horizontal blend once, preserving the original arithmetic order.
	type column struct {
		x      int
		weight float64
	}
	columns := make([]column, w)
	for x := range columns {
		sx := (float64(x)+.5)*float64(src.w)/float64(w) - .5
		x0 := int(math.Floor(sx))
		columns[x] = column{x0, sx - float64(x0)}
	}
	rows := [2][][3]float64{make([][3]float64, w), make([][3]float64, w)}
	rowY := [2]int{-2, -2}
	loadRow := func(slot, sy int) {
		if rowY[slot] == sy {
			return
		}
		for x, sample := range columns {
			a, b := get(sample.x, sy), get(sample.x+1, sy)
			for k := 0; k < 3; k++ {
				rows[slot][x][k] = a[k]*(1-sample.weight) + b[k]*sample.weight
			}
		}
		rowY[slot] = sy
	}
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return Tensor{}, tr, err
		}
		sy := (float64(y)+.5)*float64(ph)/float64(h) - .5
		y0 := int(math.Floor(sy))
		dy := sy - float64(y0)
		if rowY[1] == y0 {
			rows[0], rows[1] = rows[1], rows[0]
			rowY[0], rowY[1] = rowY[1], rowY[0]
		}
		loadRow(0, y0)
		loadRow(1, y0+1)
		for x := 0; x < w; x++ {
			for channel := 0; channel < 3; channel++ {
				p := math.Round(rows[0][x][channel]*(1-dy) + rows[1][x][channel]*dy)
				data[channel*w*h+y*w+x] = (float32(p)/255 - .5) / .5
			}
		}
	}
	return Tensor{data, shape}, tr, nil
}

func detectorSize(width, height int, c Config) (int, int, error) {
	// Every detector dimension is at least 32. Include the three input channels
	// and reserve room for that minimum even for extremely narrow selections.
	pixels := min(c.MaxDetectorPixels, c.MaxTensorElements/3)
	side := min(c.DetectorLongSide, c.MaxDetectorSide, pixels/32) / 32 * 32
	if side < 32 || pixels < 32*32 {
		return 0, 0, fmt.Errorf("%w: minimum detector input", ErrLimit)
	}
	// Keep the established short-side rounding for the balanced profile. The
	// bounded comparison instead limits enlargement and rounds cells upward.
	ratio := math.Max(1, float64(c.DetectorSide)/float64(min(width, height)))
	if c.DetectorRoundUp {
		ratio = min(ratio, float64(side)/float64(max(width, height)))
	}
	align := func(n int) int {
		if c.DetectorRoundUp {
			return max(32, (n+31)/32*32)
		}
		return max(32, int(math.RoundToEven(float64(n)/32))*32)
	}
	w := align(int(float64(width) * ratio))
	h := align(int(float64(height) * ratio))
	if w <= side && h <= side && w <= pixels/h {
		return w, h, nil
	}
	// A tighter pixel/tensor budget can still require shrinking. Round down
	// here so alignment cannot push the tensor back over its budget.
	scale := min(float64(side)/float64(w), float64(side)/float64(h),
		math.Sqrt(float64(pixels)/float64(w)/float64(h)))
	w = max(32, int(float64(w)*scale/32)*32)
	h = max(32, int(float64(h)*scale/32)*32)
	return w, h, nil
}

func bilinear(get func(int, int) [3]float64, x, y float64) [3]float64 {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	dx, dy := x-float64(x0), y-float64(y0)
	a, b, c, d := get(x0, y0), get(x0+1, y0), get(x0, y0+1), get(x0+1, y0+1)
	var p [3]float64
	for k := range p {
		p[k] = math.Round((a[k]*(1-dx)+b[k]*dx)*(1-dy) + (c[k]*(1-dx)+d[k]*dx)*dy)
	}
	return p
}

type crop struct {
	data []byte
	w, h int
}

func (c crop) at(x, y int) [3]float64 {
	x, y = min(max(x, 0), c.w-1), min(max(y, 0), c.h-1)
	i := (y*c.w + x) * 3
	return [3]float64{float64(c.data[i]), float64(c.data[i+1]), float64(c.data[i+2])}
}

func cubicWeight(x float64) float64 {
	// Cubic convolution uses coefficient -0.75. Coordinates and
	// coefficients here are floating point rather than table-quantized.
	x = math.Abs(x)
	if x <= 1 {
		return (1.25*x-2.25)*x*x + 1
	}
	if x < 2 {
		return ((-.75*x+3.75)*x-6)*x + 3
	}
	return 0
}

// projective maps a unit square onto a convex quadrilateral. This also handles
// a perspective trapezoid; bilinear interpolation of corners would not.
func projective(q Quad) ([8]float64, error) {
	var m [8]float64
	if !validQuad(q) {
		return m, fmt.Errorf("degenerate crop quad")
	}
	dx1, dx2 := q[1].X-q[2].X, q[3].X-q[2].X
	dy1, dy2 := q[1].Y-q[2].Y, q[3].Y-q[2].Y
	sx, sy := q[0].X-q[1].X+q[2].X-q[3].X, q[0].Y-q[1].Y+q[2].Y-q[3].Y
	g, h := 0.0, 0.0
	if math.Abs(sx)+math.Abs(sy) > 1e-10 {
		den := dx1*dy2 - dx2*dy1
		if math.Abs(den) < 1e-10 {
			return m, fmt.Errorf("singular crop transform")
		}
		g, h = (sx*dy2-dx2*sy)/den, (dx1*sy-sx*dy1)/den
	}
	m = [8]float64{q[1].X - q[0].X + g*q[1].X, q[3].X - q[0].X + h*q[3].X, q[0].X,
		q[1].Y - q[0].Y + g*q[1].Y, q[3].Y - q[0].Y + h*q[3].Y, q[0].Y, g, h}
	for _, uv := range [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		if m[6]*uv[0]+m[7]*uv[1]+1 <= 1e-8 {
			return m, fmt.Errorf("invalid crop projection")
		}
	}
	return m, nil
}

func cropQuad(ctx context.Context, src pixels, q Quad, c Config) (crop, error) {
	m, err := projective(q)
	if err != nil {
		return crop{}, err
	}
	w := int(max(distance(q[0], q[1]), distance(q[2], q[3])))
	h := int(max(distance(q[0], q[3]), distance(q[1], q[2])))
	if w < 1 || h < 1 {
		return crop{}, fmt.Errorf("empty crop")
	}
	if w > c.MaxCropPixels/h {
		return crop{}, fmt.Errorf("%w: crop %dx%d", ErrLimit, w, h)
	}
	rotate := float64(h)/float64(w) >= 1.5
	cw, ch := w, h
	if rotate {
		cw, ch = h, w
	}
	out := crop{make([]byte, w*h*3), cw, ch}
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return crop{}, err
		}
		for x := 0; x < w; x++ {
			u, v := float64(x)/float64(w), float64(y)/float64(h)
			den := m[6]*u + m[7]*v + 1
			sx, sy := (m[0]*u+m[1]*v+m[2])/den, (m[3]*u+m[4]*v+m[5])/den
			ix, iy := int(math.Floor(sx)), int(math.Floor(sy))
			var p [3]float64
			for by := -1; by <= 2; by++ {
				wy := cubicWeight(sy - float64(iy+by))
				for bx := -1; bx <= 2; bx++ {
					weight := wy * cubicWeight(sx-float64(ix+bx))
					pixel := src.at(ix+bx, iy+by)
					for k := range p {
						p[k] += weight * pixel[k]
					}
				}
			}
			nx, ny := x, y
			if rotate {
				// Source-to-destination mapping for 90 degrees counterclockwise,
				// matching np.rot90 with image coordinates (Y points down).
				nx, ny = y, w-1-x
			}
			for k := range p {
				out.data[(ny*cw+nx)*3+k] = byte(min(255, max(0, math.Round(p[k]))))
			}
		}
	}
	return out, nil
}

func recognitionInput(ctx context.Context, img crop, c Config) (Tensor, error) {
	if img.w <= 0 || img.h <= 0 {
		return Tensor{}, fmt.Errorf("empty recognition crop")
	}
	ratio := float64(img.w) / float64(img.h)
	w := max(320, int(48*ratio))
	if w > c.MaxRecWidth {
		return Tensor{}, fmt.Errorf("%w: recognition width %d", ErrLimit, w)
	}
	resized := min(w, int(math.Ceil(48*ratio)))
	shape := []int64{1, 3, 48, int64(w)}
	n, err := elements(shape, c.MaxTensorElements)
	if err != nil {
		return Tensor{}, err
	}
	data := make([]float32, n) // Normalized zero padding, not black (-1).
	for y := 0; y < 48; y++ {
		if err := ctx.Err(); err != nil {
			return Tensor{}, err
		}
		for x := 0; x < resized; x++ {
			p := bilinear(img.at, (float64(x)+.5)*float64(img.w)/float64(resized)-.5, (float64(y)+.5)*float64(img.h)/48-.5)
			for k := range p {
				data[k*48*w+y*w+x] = (float32(p[k])/255 - .5) / .5
			}
		}
	}
	return Tensor{data, shape}, nil
}
