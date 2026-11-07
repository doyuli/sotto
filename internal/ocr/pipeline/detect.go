package pipeline

import (
	"context"
	"fmt"
	"math"
)

type detection struct {
	quad  Quad
	score float64
}

func detect(ctx context.Context, t Tensor, tr detectorTransform, c Config) ([]detection, Diagnostics, error) {
	stats := Diagnostics{DetectorWidth: tr.width, DetectorHeight: tr.height}
	if err := validateTensor(ctx, t, 4, c.MaxTensorElements); err != nil {
		return nil, stats, err
	}
	if t.Shape[0] != 1 || t.Shape[1] != 1 {
		return nil, stats, fmt.Errorf("detector expected [1,1,H,W], got %v", t.Shape)
	}
	w, h := int(t.Shape[3]), int(t.Shape[2])
	if w > c.MaxDetectorSide || h > c.MaxDetectorSide || w > c.MaxDetectorPixels/h {
		return nil, stats, fmt.Errorf("%w: detector output", ErrLimit)
	}
	mask := make([]byte, w*h)
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, stats, err
		}
		for x := 0; x < w; x++ {
			active := float64(t.Data[y*w+x]) > c.Threshold
			if c.Dilation {
				// The 2x2 anchor is (1,1), so growth is right/down.
				for dy := 0; dy <= 1; dy++ {
					for dx := 0; dx <= 1; dx++ {
						if x >= dx && y >= dy && float64(t.Data[(y-dy)*w+x-dx]) > c.Threshold {
							active = true
						}
					}
				}
			}
			if active {
				mask[y*w+x] = 1
			}
		}
	}
	var boxes []detection
	queue := make([]int32, 0)
	boundary := make([]Point, 0)
	scoreBudget := c.MaxScorePixels
	for seed := range mask {
		if seed%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, stats, err
			}
		}
		if mask[seed] != 1 {
			continue
		}
		stats.Components++
		if stats.Components > c.MaxCandidates {
			return nil, stats, fmt.Errorf("%w: detector candidates", ErrLimit)
		}
		queue = append(queue[:0], int32(seed))
		boundary = boundary[:0]
		mask[seed] = 2
		for head := 0; head < len(queue); head++ {
			if head%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, stats, err
				}
			}
			index := int(queue[head])
			x, y := index%w, index/w
			edge := false
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						edge = true
						continue
					}
					j := ny*w + nx
					if mask[j] == 0 {
						edge = true
					} else if mask[j] == 1 {
						mask[j] = 2
						queue = append(queue, int32(j))
					}
				}
			}
			if edge {
				if len(boundary) >= c.MaxBoundaryPoints {
					return nil, stats, fmt.Errorf("%w: detector boundary", ErrLimit)
				}
				boundary = append(boundary, Point{float64(x), float64(y)})
			}
		}
		// External boundary points and the outer contour have the same convex
		// hull. Hole contours are deliberately not emitted as duplicate boxes.
		q, short := minimumRect(convexHull(boundary))
		if short < 3 {
			stats.SmallRegions++
			continue
		}
		score, err := boxScore(ctx, t.Data, w, h, q, &scoreBudget)
		if err != nil {
			return nil, stats, err
		}
		if score < c.BoxThreshold {
			stats.LowScoreRegions++
			continue
		}
		q = expandRect(q, c.UnclipRatio)
		if min(distance(q[0], q[1]), distance(q[0], q[3])) < 5 {
			stats.SmallRegions++
			continue
		}
		for i, p := range q {
			q[i] = Point{min(float64(tr.originalWidth-1), max(0, math.RoundToEven(p.X/float64(w)*float64(tr.originalWidth)))),
				min(float64(tr.originalHeight-1), max(0, math.RoundToEven(p.Y/float64(h)*float64(tr.paddedHeight))-float64(tr.padTop)))}
		}
		q = orderQuad(q)
		if !validQuad(q) || int(distance(q[0], q[1])) <= 3 || int(distance(q[0], q[3])) <= 3 {
			stats.DegenerateBoxes++
			continue
		}
		boxes = append(boxes, detection{q, score})
	}
	return boxes, stats, ctx.Err()
}

func boxScore(ctx context.Context, data []float32, w, h int, q Quad, budget *int) (float64, error) {
	b := q.bounds()
	x0, x1 := max(0, int(math.Floor(b.x0))), min(w-1, int(math.Ceil(b.x1)))
	y0, y1 := max(0, int(math.Floor(b.y0))), min(h-1, int(math.Ceil(b.y1)))
	work := (x1 - x0 + 1) * (y1 - y0 + 1)
	if work > *budget {
		return 0, fmt.Errorf("%w: detector scoring work", ErrLimit)
	}
	*budget -= work
	sum, count := 0.0, 0
	for y := y0; y <= y1; y++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		for x := x0; x <= x1; x++ {
			if inside(q, Point{float64(x), float64(y)}) {
				sum += float64(data[y*w+x])
				count++
			}
		}
	}
	if count == 0 {
		return 0, nil
	}
	return sum / float64(count), nil
}
