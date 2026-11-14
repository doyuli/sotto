package screencoords

import (
	"cmp"
	"slices"
)

type Rect struct{ X, Y, W, H int }

type Display struct {
	Pixels Rect
	DPI    int
}

// Layout keeps monitor choice and coordinate conversion in the same screen
// space, including gaps between displays with different scales.
type Layout struct {
	displays []Display
	minDPI   int
}

func NewLayout(displays []Display, fallbackDPI int) Layout {
	if fallbackDPI <= 0 {
		fallbackDPI = 96
	}
	l := Layout{displays: slices.Clone(displays), minDPI: fallbackDPI}
	for i := range l.displays {
		d := &l.displays[i]
		if d.DPI <= 0 {
			d.DPI = fallbackDPI
		}
		if i == 0 || d.DPI < l.minDPI {
			l.minDPI = d.DPI
		}
	}
	return l
}

// PixelsToDIP uses the display with the largest overlap, or the nearest
// display when the rectangle is outside all monitors, as mygo does.
func (l Layout) PixelsToDIP(x, y, w, h float64) (Rect, bool) {
	r := Rect{Scale(x, 1, 1), Scale(y, 1, 1), Scale(w, 1, 1), Scale(h, 1, 1)}
	order := l.ranked(r)
	if len(order) == 0 {
		return Rect{}, false
	}
	return l.toDIP(x, y, w, h, order[0]), true
}

func (l Layout) toDIP(x, y, w, h float64, i int) Rect {
	d := l.displays[i]
	return Rect{
		X: ToDIP(x, d.Pixels.X, Origin(d.Pixels.X, l.minDPI), d.DPI),
		Y: ToDIP(y, d.Pixels.Y, Origin(d.Pixels.Y, l.minDPI), d.DPI),
		W: Scale(w, 96, d.DPI), H: Scale(h, 96, d.DPI),
	}
}

// ranked follows mygo's internal/windows.byOverlap ordering: descending
// overlap, then ascending squared rectangle distance, with stable ties.
func (l Layout) ranked(r Rect) []int {
	r.W, r.H = max(r.W, 1), max(r.H, 1)
	type rank struct {
		index          int
		area, distance int64
	}
	ranks := make([]rank, len(l.displays))
	for i, d := range l.displays {
		b := d.Pixels
		w := min(r.X+r.W, b.X+b.W) - max(r.X, b.X)
		h := min(r.Y+r.H, b.Y+b.H) - max(r.Y, b.Y)
		v := rank{index: i}
		if w > 0 && h > 0 {
			v.area = int64(w) * int64(h)
		} else {
			dx, dy := int64(max(0, -w)), int64(max(0, -h))
			v.distance = dx*dx + dy*dy
		}
		ranks[i] = v
	}
	slices.SortStableFunc(ranks, func(a, b rank) int {
		if order := cmp.Compare(b.area, a.area); order != 0 {
			return order
		}
		return cmp.Compare(a.distance, b.distance)
	})
	order := make([]int, len(ranks))
	for i, r := range ranks {
		order[i] = r.index
	}
	return order
}
