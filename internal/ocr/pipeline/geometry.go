package pipeline

import (
	"math"
	"sort"
)

func distance(a, b Point) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }
func cross(a, b, c Point) float64 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }
func dot(a, b Point) float64      { return a.X*b.X + a.Y*b.Y }

func validQuad(q Quad) bool {
	for _, p := range q {
		if !finite(p.X) || !finite(p.Y) {
			return false
		}
	}
	sign := 0.0
	for i := range q {
		v := cross(q[i], q[(i+1)%4], q[(i+2)%4])
		if math.Abs(v) < 1e-8 {
			return false
		}
		if sign*v < 0 {
			return false
		}
		sign = v
	}
	return true
}

func convexHull(points []Point) []Point {
	sort.Slice(points, func(i, j int) bool {
		if points[i].X != points[j].X {
			return points[i].X < points[j].X
		}
		return points[i].Y < points[j].Y
	})
	hull := make([]Point, 0, len(points)*2)
	for _, p := range points {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull)
	for i := len(points) - 2; i >= 0; i-- {
		p := points[i]
		for len(hull) > lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	if len(hull) > 1 {
		hull = hull[:len(hull)-1]
	}
	return hull
}

func orderQuad(q Quad) Quad {
	sort.Slice(q[:], func(i, j int) bool {
		if q[i].X != q[j].X {
			return q[i].X < q[j].X
		}
		return q[i].Y < q[j].Y
	})
	if q[0].Y > q[1].Y {
		q[0], q[1] = q[1], q[0]
	}
	if q[2].Y > q[3].Y {
		q[2], q[3] = q[3], q[2]
	}
	return Quad{q[0], q[2], q[3], q[1]}
}

// Rotating supporting lines visit each convex hull vertex a bounded number of
// times, avoiding a quadratic scan for every contour edge.
func minimumRect(hull []Point) (Quad, float64) {
	if len(hull) < 3 {
		return Quad{}, 0
	}
	indices := [4]int{}
	bestArea := math.Inf(1)
	var best Quad
	short := 0.0
	for edge, a := range hull {
		b := hull[(edge+1)%len(hull)]
		length := distance(a, b)
		if length < 1e-10 {
			continue
		}
		u := Point{(b.X - a.X) / length, (b.Y - a.Y) / length}
		v := Point{-u.Y, u.X}
		directions := [4]Point{u, {-u.X, -u.Y}, v, {-v.X, -v.Y}}
		var extent [4]float64
		for k, d := range directions {
			if edge == 0 {
				for j := 1; j < len(hull); j++ {
					if dot(hull[j], d) > dot(hull[indices[k]], d) {
						indices[k] = j
					}
				}
			} else {
				for steps := 0; steps < len(hull); steps++ {
					next := (indices[k] + 1) % len(hull)
					if dot(hull[next], d) <= dot(hull[indices[k]], d)+1e-10 {
						break
					}
					indices[k] = next
				}
			}
			extent[k] = dot(hull[indices[k]], d)
		}
		loU, hiU, loV, hiV := -extent[1], extent[0], -extent[3], extent[2]
		w, h := hiU-loU, hiV-loV
		if w*h < bestArea-1e-8 {
			bestArea, short = w*h, min(w, h)
			point := func(x, y float64) Point { return Point{u.X*x + v.X*y, u.Y*x + v.Y*y} }
			best = orderQuad(Quad{point(loU, loV), point(hiU, loV), point(hiU, hiV), point(loU, hiV)})
		}
	}
	return best, short
}

// A rectangle's round offset has the same minimum bounding rectangle as
// extending each side by d. Keep the continuous coordinates until mapping.
func expandRect(q Quad, ratio float64) Quad {
	w, h := distance(q[0], q[1]), distance(q[0], q[3])
	if w == 0 || h == 0 {
		return Quad{}
	}
	d := w * h * ratio / (2 * (w + h))
	u := Point{(q[1].X - q[0].X) / w * d, (q[1].Y - q[0].Y) / w * d}
	v := Point{(q[3].X - q[0].X) / h * d, (q[3].Y - q[0].Y) / h * d}
	return Quad{{q[0].X - u.X - v.X, q[0].Y - u.Y - v.Y}, {q[1].X + u.X - v.X, q[1].Y + u.Y - v.Y},
		{q[2].X + u.X + v.X, q[2].Y + u.Y + v.Y}, {q[3].X - u.X + v.X, q[3].Y - u.Y + v.Y}}
}

type bounds struct{ x0, y0, x1, y1 float64 }

func (q Quad) bounds() bounds {
	b := bounds{q[0].X, q[0].Y, q[0].X, q[0].Y}
	for _, p := range q {
		b.x0 = min(b.x0, p.X)
		b.y0 = min(b.y0, p.Y)
		b.x1 = max(b.x1, p.X)
		b.y1 = max(b.y1, p.Y)
	}
	return b
}
func (b bounds) width() float64  { return b.x1 - b.x0 }
func (b bounds) height() float64 { return b.y1 - b.y0 }
func (b bounds) union(c bounds) bounds {
	return bounds{min(b.x0, c.x0), min(b.y0, c.y0), max(b.x1, c.x1), max(b.y1, c.y1)}
}

func inside(q Quad, p Point) bool {
	positive, negative := false, false
	for i := range q {
		v := cross(q[i], q[(i+1)%4], p)
		positive = positive || v > 1e-8
		negative = negative || v < -1e-8
	}
	return !(positive && negative)
}
