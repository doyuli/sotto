//go:build !windows

package ocr

import (
	"context"
	"errors"
	"image"
)

var errUnsupported = errors.New("ocr is only supported on Windows")

func recognize(context.Context, image.Image) (Text, error)             { return Text{}, errUnsupported }
func recognizeWith(context.Context, image.Image, string) (Text, error) { return Text{}, errUnsupported }
func availableLanguages(context.Context) []string                      { return nil }
