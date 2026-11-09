// Package screencoords maps Windows physical pixels to mygo's screen DIPs.
package screencoords

import "math"

// Scale rounds halves up, including negative screen coordinates.
func Scale(value float64, numerator, denominator int) int {
	return int(math.Floor(value*float64(numerator)/float64(denominator) + 0.5))
}

// Origin anchors every display using the smallest DPI in the display layout.
// Only offsets within a display use that display's own DPI, keeping displays
// with different scales from overlapping in DIP space.
func Origin(pixelOrigin, minDPI int) int {
	return Scale(float64(pixelOrigin), 96, minDPI)
}

// ToDIP converts a physical coordinate relative to its display's origin.
func ToDIP(pixel float64, pixelOrigin, dipOrigin, dpi int) int {
	return dipOrigin + Scale(pixel-float64(pixelOrigin), 96, dpi)
}

// ToPixels converts a DIP coordinate relative to its display's origin.
func ToPixels(dip, pixelOrigin, dipOrigin, dpi int) int {
	return pixelOrigin + Scale(float64(dip-dipOrigin), dpi, 96)
}
