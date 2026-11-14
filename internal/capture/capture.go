// Package capture grabs a rectangle of the screen as an image, for the
// OCR pipeline. Coordinates follow mygo's logical screen space; platform
// layers convert the chosen display's local units to capture pixels.
package capture

// Rect uses overlay-local DIPs on Windows and display-local points on macOS,
// as defined by GrabDisplayWindow.
type Rect struct{ X, Y, W, H int }
