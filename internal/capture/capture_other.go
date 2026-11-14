//go:build !windows && !darwin

package capture

import (
	"errors"
	"image"
)

func grabWindow(uintptr, Rect) (image.Image, error) {
	return nil, errors.New("capture is only supported on Windows and macOS")
}
