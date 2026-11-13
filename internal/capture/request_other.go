//go:build !darwin

package capture

import (
	"context"
	"image"
)

func grabDisplayWindow(ctx context.Context, handle uintptr, _ int64, rect Rect) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	img, err := grabWindow(handle, rect)
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	return img, err
}
