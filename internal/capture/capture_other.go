//go:build !windows && !darwin

package capture

import (
	"errors"
	"image"
)

func grab(Rect) (image.Image, error) {
	return nil, errors.New("capture is only supported on Windows")
}

func grabWindow(_ uintptr, rect Rect) (image.Image, error) { return grab(rect) }
