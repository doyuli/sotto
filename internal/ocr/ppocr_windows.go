//go:build windows

package ocr

import (
	"context"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"time"

	"sotto/internal/ocr/pipeline"
)

// The public admission gate serializes initialization and inference. Failed
// initialization is retried on the next request, allowing repaired resources.
var ppEngine *pipeline.Recognizer

func recognizePPOCR(ctx context.Context, img image.Image, _ string) (Text, error) {
	t := Text{Engine: "ppocr-v6-small"}
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	if ppEngine == nil {
		start := time.Now()
		r, err := newPPOCR(ctx)
		t.timings.InitializationUS = time.Since(start).Microseconds()
		if err != nil {
			return t, err
		}
		ppEngine = r
	}
	result, err := ppEngine.Recognize(ctx, img)
	t.Diagnostics, t.timings.Pipeline = result.Diagnostics, result.Timings
	if err != nil {
		return t, err
	}
	t.Lines = result.Lines
	return t, nil
}
func newPPOCR(ctx context.Context) (*pipeline.Recognizer, error) {
	c, err := detectorConfig(os.Getenv("SOTTO_OCR_PROFILE"))
	if err != nil {
		return nil, err
	}
	threads, err := detectorThreads(os.Getenv("SOTTO_OCR_DETECTOR_THREADS"))
	if err != nil {
		return nil, err
	}
	log.Printf("ocr: local configuration shortSide=%d longSide=%d roundUp=%t maxPixels=%d detectorThreads=%d", c.DetectorSide, c.DetectorLongSide, c.DetectorRoundUp, c.MaxDetectorPixels, threads)
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
	det, err := newORTSessionWithThreads(filepath.Join(dir, m.Models[0].File), m.Models[0].InputName, m.Models[0].OutputName, threads)
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
	r, err := pipeline.New(det, rec, dictionary, c)
	if err != nil {
		return nil, err
	}
	success = true
	return r, nil
}
