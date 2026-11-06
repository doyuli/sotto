//go:build !windows

package capture

import (
	"errors"
	"image"
)

func grab(Rect) (image.Image, error) {
	return nil, errors.New("capture is only supported on Windows")
}
