//go:build darwin

package snip

import (
	"github.com/egoist/mygo"

	"sotto/internal/capture"
)

func captureRect(content, display mygo.Rectangle, r *Rect) capture.Rect {
	// AppKit constrains the overlay below the menu bar, even when its title
	// bar is hidden. SCK needs display-local points, not overlay-local points.
	// Use the actual content origin: menu bar heights differ across displays.
	return capture.Rect{
		X: content.X - display.X + r.X,
		Y: content.Y - display.Y + r.Y,
		W: r.W,
		H: r.H,
	}
}
