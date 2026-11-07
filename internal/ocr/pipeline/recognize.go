package pipeline

import (
	"context"
	"fmt"
	"image"
)

type Recognizer struct {
	det, rec Runner
	labels   []string
	config   Config
}

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
	return &Recognizer{det, rec, labels, c}, nil
}

// Recognize retains low-confidence lines. An error never returns a partial
// success; diagnostics accumulated before failure are still available.
func (r *Recognizer) Recognize(ctx context.Context, img image.Image) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	src, err := source(img, r.config)
	if err != nil {
		return result, fmt.Errorf("ocr input: %w", err)
	}
	input, tr, err := detectorInput(ctx, src, r.config)
	if err != nil {
		return result, fmt.Errorf("ocr detection preprocessing: %w", err)
	}
	out, err := run(ctx, r.det, input)
	if err != nil {
		return result, fmt.Errorf("ocr detection inference: %w", err)
	}
	boxes, stats, err := detect(ctx, out, tr, r.config)
	result.Diagnostics = stats
	if err != nil {
		return result, fmt.Errorf("ocr detection postprocessing: %w", err)
	}
	lines := make([]Line, 0, len(boxes))
	for i, box := range boxes {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		cropped, err := cropQuad(ctx, src, box.quad, r.config)
		if err != nil {
			return result, fmt.Errorf("ocr crop %d: %w", i, err)
		}
		input, err := recognitionInput(ctx, cropped, r.config)
		if err != nil {
			return result, fmt.Errorf("ocr recognition preprocessing %d: %w", i, err)
		}
		out, err := run(ctx, r.rec, input)
		if err != nil {
			return result, fmt.Errorf("ocr recognition inference %d: %w", i, err)
		}
		if len(out.Shape) != 3 || out.Shape[0] != 1 {
			return result, fmt.Errorf("ocr recognition expected [1,T,C], got %v", out.Shape)
		}
		decoded, err := decode(ctx, out, r.labels, r.config.MaxTensorElements)
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
