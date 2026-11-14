// Package capture grabs a rectangle of the screen as an image, for the
// OCR pipeline. Coordinates follow mygo's logical screen space; platform
// layers convert the chosen display's local units to capture pixels.
package capture

import "image"

// Grab returns the screen content of rect (DIP coordinates, as mygo's
// windows place).
func Grab(rect Rect) (image.Image, error) {
	return grab(rect)
}

// Rect is a rectangle in DIP screen coordinates.
type Rect struct{ X, Y, W, H int }
