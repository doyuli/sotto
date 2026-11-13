package snip

import (
	"math"

	"github.com/egoist/mygo"
)

type Rect struct{ X, Y, W, H int }
type snipPoint struct{ x, y float32 }

// Both the selection and capture are relative to the client area, which can
// differ from the outer window bounds if the window's frame style changes.
func snipCaptureAnchor(content mygo.Rectangle, r *Rect) *mygo.Point {
	if r == nil || r.W < 8 || r.H < 8 || r.X < 0 || r.Y < 0 ||
		r.X > content.Width-r.W || r.Y > content.Height-r.H {
		return nil
	}
	return &mygo.Point{X: content.X + r.X, Y: content.Y + r.Y + r.H}
}

// Selection stays in overlay-local logical units. The capture boundary maps
// these to display-local points on macOS; Windows resolves them through the HWND.
type snipSelection struct {
	width, height float32
	start, end    snipPoint
	dragging      bool
	submitted     bool
}

func (s *snipSelection) point(x, y float32) snipPoint {
	return snipPoint{max(0, min(s.width, x)), max(0, min(s.height, y))}
}

func (s *snipSelection) begin(x, y float32) bool {
	if s.submitted || s.dragging {
		return false
	}
	s.start = s.point(x, y)
	s.end, s.dragging = s.start, true
	return true
}

func (s *snipSelection) move(x, y float32) bool {
	if !s.dragging || s.submitted {
		return false
	}
	s.end = s.point(x, y)
	return true
}

func (s *snipSelection) bounds() (x, y, w, h float32) {
	return min(s.start.x, s.end.x), min(s.start.y, s.end.y),
		max(s.start.x, s.end.x) - min(s.start.x, s.end.x),
		max(s.start.y, s.end.y) - min(s.start.y, s.end.y)
}

func (s *snipSelection) finish(x, y float32, canceled bool) (*Rect, bool) {
	if !s.dragging || s.submitted {
		return nil, false
	}
	s.move(x, y)
	x, y, w, h := s.bounds()
	s.dragging, s.submitted = false, true
	if canceled || w < 8 || h < 8 {
		return nil, true
	}
	rx, ry := roundSnip(x), roundSnip(y)
	// Rounding the origin and extent separately can otherwise add a DIP
	// beyond the right/bottom edge for a half-DIP pointer position.
	rw, rh := min(roundSnip(w), roundSnip(s.width)-rx), min(roundSnip(h), roundSnip(s.height)-ry)
	if rw < 8 || rh < 8 {
		return nil, true
	}
	return &Rect{rx, ry, rw, rh}, true
}

func (s *snipSelection) cancel() bool {
	if s.submitted {
		return false
	}
	s.dragging, s.submitted = false, true
	return true
}

func roundSnip(v float32) int { return int(math.Round(float64(v))) }
