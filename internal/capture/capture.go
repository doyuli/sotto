// Package capture grabs a rectangle of the screen as an image, for the
// OCR pipeline. Coordinates follow mygo's DIP screen space; the Windows
// layer converts them to the physical pixels GDI captures in.
package capture

import "image"

// Grab returns the screen content of rect (DIP coordinates, as mygo's
// windows place).
func Grab(rect Rect) (image.Image, error) {
	return grab(rect)
}

// Rect is a rectangle in DIP screen coordinates.
type Rect struct{ X, Y, W, H int }
