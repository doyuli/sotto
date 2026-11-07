//go:build !windows

package ocr

import (
	"context"
	"errors"
	"image"
)

var errUnsupported = errors.New("ocr is only supported on Windows")

func recognizeWith(context.Context, image.Image, string) (Text, error) { return Text{}, errUnsupported }
