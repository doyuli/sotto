package capture

import (
	"context"
	"image"
)

// GrabDisplayWindow uses overlay-local DIPs on Windows (the leased HWND resolves
// its client origin and DPI), and display-local points on macOS with a stable ID.
func GrabDisplayWindow(ctx context.Context, handle uintptr, displayID int64, rect Rect) (image.Image, error) {
	return grabDisplayWindow(ctx, handle, displayID, rect)
}
