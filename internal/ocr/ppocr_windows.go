//go:build windows

package ocr

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"

	"sotto/internal/ocr/pipeline"
)

// The public admission gate serializes initialization and inference. Failed
// initialization is retried on the next request, allowing repaired resources.
var ppEngine *pipeline.Recognizer

func recognizePPOCR(ctx context.Context, img image.Image, _ string) (Text, error) {
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	if ppEngine == nil {
		r, err := newPPOCR(ctx)
		if err != nil {
			return Text{}, err
		}
		ppEngine = r
	}
	result, err := ppEngine.Recognize(ctx, img)
	if err != nil {
		return Text{}, err
	}
	return Text{Lines: result.Lines, Diagnostics: result.Diagnostics, Engine: "ppocr-v6-small"}, nil
}
func newPPOCR(ctx context.Context) (*pipeline.Recognizer, error) {
	m, err := readManifest()
	if err != nil {
		return nil, err
	}
	dir, _ := ocrResourcePaths()
	for _, asset := range m.Assets {
		if err := verifyAsset(ctx, dir, asset); err != nil {
			return nil, err
		}
	}
	dictionary, err := os.ReadFile(filepath.Join(dir, m.Dictionary.File))
	if err != nil {
		return nil, fmt.Errorf("ocr: read dictionary: %w", err)
	}
	det, err := newORTSession(filepath.Join(dir, m.Models[0].File), m.Models[0].InputName, m.Models[0].OutputName)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			det.close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rec, err := newORTSession(filepath.Join(dir, m.Models[1].File), m.Models[1].InputName, m.Models[1].OutputName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !success {
			rec.close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := pipeline.New(det, rec, dictionary, pipeline.DefaultConfig())
	if err != nil {
		return nil, err
	}
	success = true
	return r, nil
}
