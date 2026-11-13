//go:build !darwin

package snip

import (
	"github.com/egoist/mygo"

	"sotto/internal/capture"
)

func captureRect(_, _ mygo.Rectangle, r *Rect) capture.Rect {
	// Windows GrabWindow resolves the client origin and DPI from the leased
	// HWND. Keep its input in overlay-local DIPs to avoid applying it twice.
	return capture.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
}
