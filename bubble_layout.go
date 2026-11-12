package main

import (
	"math"

	"github.com/egoist/mygo"
)

const (
	bubbleMargin    = 12
	bubbleGap       = 16
	bubbleMaxHeight = 760
)

// bubblePlacement keeps the chosen side stable while asynchronous results
// change the height. An upward-opening bubble grows away from the selection.
type bubblePlacement struct {
	displayID          int64
	work               mygo.Rectangle
	point              mygo.Point
	input, above, left bool
}

func newBubblePlacement(display mygo.Display, point mygo.Point, input bool, width int) bubblePlacement {
	work := display.WorkArea
	below := work.Y + work.Height - bubbleMargin - point.Y - bubbleGap
	above := point.Y - bubbleGap - work.Y - bubbleMargin
	return bubblePlacement{
		displayID: display.ID, work: work, point: point, input: input,
		above: below < min(480, bubbleHeightLimit(work)) && above > below,
		left:  point.X+bubbleGap+width > work.X+work.Width-bubbleMargin,
	}
}

// Keep the anchor's display through content growth, even if the temporarily
// taller window crosses another display. Removed displays fall back to the
// window's nearest live display; changed work areas invalidate the old sides.
func (p *bubblePlacement) refresh(displays []mygo.Display, fallback mygo.Display, width int) {
	display := fallback
	for _, candidate := range displays {
		if candidate.ID == p.displayID {
			display = candidate
			break
		}
	}
	if display.ID == p.displayID && display.WorkArea == p.work {
		return
	}
	work := display.WorkArea
	point := mygo.Point{
		X: min(max(p.point.X, work.X), work.X+max(work.Width-1, 0)),
		Y: min(max(p.point.Y, work.Y), work.Y+max(work.Height-1, 0)),
	}
	*p = newBubblePlacement(display, point, p.input, width)
}

func bubbleHeightLimit(work mygo.Rectangle) int {
	return max(1, min(bubbleMaxHeight, work.Height-2*bubbleMargin))
}

func (p bubblePlacement) position(width, height int) (int, int) {
	x, y := p.point.X+bubbleGap, p.point.Y+bubbleGap
	if p.left {
		x = p.point.X - bubbleGap - width
	}
	if p.above {
		y = p.point.Y - bubbleGap - height
	}
	if p.input {
		x = p.work.X + (p.work.Width-width)/2
		y = p.work.Y + p.work.Height/4
	}
	return clampBubblePosition(p.work, x, y, width, height)
}

func clampBubblePosition(work mygo.Rectangle, x, y, width, height int) (int, int) {
	marginX := min(bubbleMargin, max((work.Width-width)/2, 0))
	marginY := min(bubbleMargin, max((work.Height-height)/2, 0))
	return min(max(x, work.X+marginX), work.X+max(work.Width-width-marginX, 0)),
		min(max(y, work.Y+marginY), work.Y+max(work.Height-height-marginY, 0))
}

// bubblePosition tracks actual geometry, not event counts: a native move
// notification may arrive after SetPosition returned or after another move.
type bubblePosition struct {
	point    mygo.Point
	displays []mygo.Display
	known    bool
}

func (p *bubblePosition) remember(bounds mygo.Rectangle, displays []mygo.Display) {
	p.point = mygo.Point{X: bounds.X, Y: bounds.Y}
	p.displays = append(p.displays[:0], displays...)
	p.known = true
}

func sameBubbleDisplays(before, after []mygo.Display) bool {
	if len(before) != len(after) {
		return false
	}
	for _, old := range before {
		found := false
		for _, current := range after {
			if old == current {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// A changed display configuration can relocate a window without user input.
// Crossing between unchanged displays, however, is still a manual move.
func (p *bubblePosition) moved(bounds mygo.Rectangle, displays []mygo.Display) bool {
	moved := p.known && p.point != (mygo.Point{X: bounds.X, Y: bounds.Y}) && sameBubbleDisplays(p.displays, displays)
	p.remember(bounds, displays)
	return moved
}

// Native callbacks can run inside these calls; delayed callbacks instead see
// the actual resulting position recorded before the main thread yields.
func (b *bubble) changeGeometry(change func()) {
	adjusting := b.adjustingLayout
	b.adjustingLayout = true
	defer func() { b.adjustingLayout = adjusting }()
	change()
	b.positionState.remember(b.win.Bounds(), mygo.Screen.Displays())
}

func (b *bubble) setPosition(x, y int) {
	b.changeGeometry(func() { b.win.SetPosition(x, y) })
}

func (b *bubble) setContentSize(width, height int) {
	b.changeGeometry(func() { b.win.SetContentSize(width, height) })
}

// reflow runs on the main thread. Manually moved windows retain their position;
// only automatic content growth is kept inside the current work area.
func (m *bubbleManager) reflow(b *bubble, resize bool) {
	if b.adjustingLayout {
		return
	}
	b.adjustingLayout = true
	defer func() { b.adjustingLayout = false }()
	bounds := b.win.Bounds()
	displays := mygo.Screen.Displays()
	if b.positionState.moved(bounds, displays) {
		b.placement = nil
	}
	display := mygo.Screen.DisplayNearestPoint(mygo.Point{X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2})
	work := display.WorkArea
	m.mu.Lock()
	pinned := b.pinned
	m.mu.Unlock()
	if !pinned && b.placement != nil {
		b.placement.refresh(displays, display, bounds.Width)
		work = b.placement.work
	}
	limit := float64(bubbleHeightLimit(work))
	m.mu.Lock()
	changed := b.maxHeight != limit
	b.maxHeight = limit
	view := b.view
	m.mu.Unlock()
	if changed && view != nil {
		view.setMaxHeight(limit)
		b.win.Invalidate()
	}
	if !resize {
		return
	}
	x, y := bounds.X, bounds.Y
	if pinned || b.placement == nil {
		x, y = clampBubblePosition(work, x, y, bounds.Width, bounds.Height)
	} else {
		x, y = b.placement.position(bounds.Width, bounds.Height)
	}
	if x != bounds.X || y != bounds.Y {
		b.setPosition(x, y)
	}
}

// A hidden window must also be reconciled before its next presentation; a
// visible bubble cannot rely on another translation causing a native resize.
func (m *bubbleManager) refreshDisplays() {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		bubbles := make([]*bubble, 0, len(m.byID))
		for _, b := range m.byID {
			bubbles = append(bubbles, b)
		}
		m.mu.Unlock()
		for _, b := range bubbles {
			m.reflow(b, true)
		}
	})
}

func bubbleLayoutHeight(height, limit float64) int {
	// Clamp before conversion and round fractional native view sizes as mygo does.
	h := int(math.Ceil(min(min(height, limit), bubbleMaxHeight) - 1.0/128))
	return max(160, h)
}
