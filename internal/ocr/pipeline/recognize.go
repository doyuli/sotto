package pipeline

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"time"
)

type Recognizer struct {
	det, rec Runner
	labels   []string
	config   Config
	closed   bool
}

// New takes ownership of two distinct runners on success. The caller retains
// ownership on failure and must clean up any partially initialized sessions.
func New(det, rec Runner, dictionary []byte, c Config) (*Recognizer, error) {
	if det == nil || rec == nil {
		return nil, fmt.Errorf("ocr pipeline: both runners are required")
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	labels, err := ParseDictionary(dictionary)
	if err != nil {
		return nil, err
	}
	return &Recognizer{det: det, rec: rec, labels: labels, config: c}, nil
}

// Close consumes both runners' ownership, even if one closer fails. The
// owner must serialize Close with Recognize; a closed recognizer is terminal.
func (r *Recognizer) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	var errs []error
	for _, runner := range []struct {
		name string
		run  Runner
	}{{"detector", r.det}, {"recognizer", r.rec}} {
		if closer, ok := runner.run.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("ocr close %s: %w", runner.name, err))
			}
		}
	}
	r.det, r.rec, r.labels = nil, nil, nil
	return errors.Join(errs...)
}

// Recognize retains low-confidence lines. An error never returns a partial
// success; diagnostics accumulated before failure are still available.
func (r *Recognizer) Recognize(ctx context.Context, img image.Image) (Result, error) {
	var result Result
	if r.closed {
		return result, fmt.Errorf("ocr pipeline: recognizer closed")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	stage := time.Now()
	src, err := source(img, r.config)
	if err != nil {
		return result, fmt.Errorf("ocr input: %w", err)
	}
	input, tr, err := detectorInput(ctx, src, r.config)
	result.Timings.DetectionPreprocessUS = time.Since(stage).Microseconds()
	result.Diagnostics.DetectorWidth, result.Diagnostics.DetectorHeight = tr.width, tr.height
	if err != nil {
		return result, fmt.Errorf("ocr detection preprocessing: %w", err)
	}
	stage = time.Now()
	out, err := run(ctx, r.det, input)
	result.Timings.DetectionInferenceUS = time.Since(stage).Microseconds()
	if err != nil {
		return result, fmt.Errorf("ocr detection inference: %w", err)
	}
	stage = time.Now()
	boxes, stats, err := detect(ctx, out, tr, r.config)
	result.Timings.DetectionPostprocessUS = time.Since(stage).Microseconds()
	result.Diagnostics = stats
	if err != nil {
		return result, fmt.Errorf("ocr detection postprocessing: %w", err)
	}
	lines := make([]Line, 0, len(boxes))
	for i, box := range boxes {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		stage = time.Now()
		cropped, err := cropQuad(ctx, src, box.quad, r.config)
		result.Timings.CropUS += time.Since(stage).Microseconds()
		if err != nil {
			return result, fmt.Errorf("ocr crop %d: %w", i, err)
		}
		stage = time.Now()
		input, err := recognitionInput(ctx, cropped, r.config)
		result.Timings.RecognitionPreprocessUS += time.Since(stage).Microseconds()
		if err != nil {
			return result, fmt.Errorf("ocr recognition preprocessing %d: %w", i, err)
		}
		stage = time.Now()
		result.Diagnostics.RecognitionCalls++
		out, err := run(ctx, r.rec, input)
		result.Timings.RecognitionInferenceUS += time.Since(stage).Microseconds()
		if err != nil {
			return result, fmt.Errorf("ocr recognition inference %d: %w", i, err)
		}
		if len(out.Shape) != 3 || out.Shape[0] != 1 {
			return result, fmt.Errorf("ocr recognition expected [1,T,C], got %v", out.Shape)
		}
		stage = time.Now()
		decoded, err := decode(ctx, out, r.labels, r.config.MaxTensorElements)
		result.Timings.DecodeUS += time.Since(stage).Microseconds()
		if err != nil {
			return result, fmt.Errorf("ocr CTC %d: %w", i, err)
		}
		d := decoded[0]
		if d.text == "" {
			result.Diagnostics.EmptyLines++
		}
		lines = append(lines, Line{Text: d.text, Quad: box.quad, Score: d.score, HasScore: true, DetectionScore: box.score, Tokens: d.tokens})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Lines = lines
	return result, nil
}

func run(ctx context.Context, r Runner, input Tensor) (Tensor, error) {
	if err := ctx.Err(); err != nil {
		return Tensor{}, err
	}
	out, err := r.Run(ctx, input)
	if cancel := ctx.Err(); cancel != nil {
		return Tensor{}, cancel
	}
	return out, err
}
