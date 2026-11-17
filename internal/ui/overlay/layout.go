package overlay

import "github.com/egoist/mygo"

const (
	maxWidth   = 320
	maxLines   = 6
	lineHeight = 23
	paddingX   = 10
	paddingY   = 6
	gap        = 8
	margin     = 12
)

type size struct{ width, height int }

func limits(work mygo.Rectangle) size {
	return size{
		width:  max(1, min(maxWidth, work.Width-2*margin)),
		height: max(1, min(maxLines*lineHeight+2*paddingY, work.Height-2*margin)),
	}
}

func fit(work, rect mygo.Rectangle) mygo.Rectangle {
	mx := max(0, min(margin, (work.Width-rect.Width)/2))
	my := max(0, min(margin, (work.Height-rect.Height)/2))
	rect.X = min(max(rect.X, work.X+mx), work.X+max(mx, work.Width-rect.Width-mx))
	rect.Y = min(max(rect.Y, work.Y+my), work.Y+max(my, work.Height-rect.Height-my))
	return rect
}

// Reserve a stable native frame around the visible panel. Grow away from the
// source when possible, flipping at the work-area edge to preserve placement.
func reservePanel(work, visible mygo.Rectangle, limit size, right, bottom bool) (mygo.Rectangle, mygo.Point, bool, bool) {
	visible.Width, visible.Height = min(visible.Width, limit.width), min(visible.Height, limit.height)
	visible = fit(work, visible)
	frame := mygo.Rectangle{X: visible.X, Y: visible.Y, Width: limit.width, Height: limit.height}
	if right {
		frame.X += visible.Width - frame.Width
	}
	if bottom {
		frame.Y += visible.Height - frame.Height
	}
	frame = fit(work, frame)
	inset := mygo.Point{X: visible.X - frame.X, Y: visible.Y - frame.Y}
	right = right || inset.X > 0
	bottom = bottom || inset.Y > 0
	if right {
		inset.X = frame.Width - inset.X - visible.Width
	}
	if bottom {
		inset.Y = frame.Height - inset.Y - visible.Height
	}
	return frame, inset, right, bottom
}

func overlap(a, b mygo.Rectangle) int {
	return max(0, min(a.X+a.Width, b.X+b.Width)-max(a.X, b.X)) *
		max(0, min(a.Y+a.Height, b.Y+b.Height)-max(a.Y, b.Y))
}

func overlapResults(rect mygo.Rectangle, occupied []mygo.Rectangle) int {
	area := 0
	for _, other := range occupied {
		area += overlap(rect, other)
	}
	return area
}

func initialPanel(work, source mygo.Rectangle, measured, limit size, occupied []mygo.Rectangle) mygo.Rectangle {
	panel := place(work, source, measured, occupied)
	right, bottom := panel.X+panel.Width <= source.X, panel.Y+panel.Height <= source.Y
	frame, _, _, _ := reservePanel(work, panel, limit, right, bottom)
	// Prefer room for the stream to grow when that avoids covering the source.
	alternative := place(work, source, limit, occupied)
	if overlap(frame, source) > 0 && overlap(alternative, source) == 0 {
		panel = alternative
		if panel.X+panel.Width <= source.X {
			panel.X += panel.Width - measured.width
		}
		if panel.Y+panel.Height <= source.Y {
			panel.Y += panel.Height - measured.height
		}
		panel.Width, panel.Height = measured.width, measured.height
	}
	return panel
}

func place(work, source mygo.Rectangle, s size, occupied []mygo.Rectangle) mygo.Rectangle {
	points := []mygo.Point{
		{X: source.X, Y: source.Y + source.Height + gap},
		{X: source.X, Y: source.Y - s.height - gap},
		{X: source.X + source.Width + gap, Y: source.Y},
		{X: source.X - s.width - gap, Y: source.Y},
	}
	// Try the spaces immediately next to another result before settling on overlap.
	for _, r := range occupied {
		points = append(points,
			mygo.Point{X: r.X + r.Width + gap, Y: r.Y},
			mygo.Point{X: r.X - s.width - gap, Y: r.Y},
			mygo.Point{X: r.X, Y: r.Y + r.Height + gap},
			mygo.Point{X: r.X, Y: r.Y - s.height - gap},
		)
	}
	var best mygo.Rectangle
	bestSource, bestOccupied, bestDistance := -1, 0, int64(0)
	for _, point := range points {
		r := fit(work, mygo.Rectangle{X: point.X, Y: point.Y, Width: s.width, Height: s.height})
		sourceArea, occupiedArea := overlap(r, source), overlapResults(r, occupied)
		dx, dy := int64(r.X-source.X), int64(r.Y-source.Y)
		distance := dx*dx + dy*dy
		if bestSource < 0 || sourceArea < bestSource ||
			sourceArea == bestSource && (occupiedArea < bestOccupied || occupiedArea == bestOccupied && distance < bestDistance) {
			best, bestSource, bestOccupied, bestDistance = r, sourceArea, occupiedArea, distance
		}
	}
	return best
}
