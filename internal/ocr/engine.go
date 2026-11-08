package ocr

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"slices"
	"strings"
	"time"
	"unicode"

	"sotto/internal/ocr/pipeline"
)

type recognizeFunc func(context.Context, image.Image, string) (Text, error)

func recognizeEngines(ctx context.Context, img image.Image, lang string, local, system recognizeFunc) (result Text, err error) {
	var attempts []EngineAttempt
	defer func() { result.Attempts = attempts }()
	run := func(name string, engine recognizeFunc) (Text, error) {
		start := time.Now()
		t, err := runEngine(ctx, img, lang, engine)
		attempt := EngineAttempt{Engine: name, ElapsedUS: time.Since(start).Microseconds(), Timings: t.timings, Diagnostics: t.Diagnostics}
		for _, risk := range t.Risks {
			if !slices.Contains(attempt.Risks, risk.Reason) {
				attempt.Risks = append(attempt.Risks, risk.Reason)
			}
		}
		if err != nil {
			attempt.Error = err.Error()
		}
		attempts = append(attempts, attempt)
		return t, err
	}
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	// The bundled model is multilingual. A language hint must not bypass it;
	// retain the hint for system OCR if local recognition needs a fallback.
	first, firstErr := run("ppocr-v6-small", local)
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	if errors.Is(firstErr, context.Canceled) || errors.Is(firstErr, context.DeadlineExceeded) {
		return Text{}, firstErr
	}
	if firstErr == nil && strings.TrimSpace(first.Text) != "" && !hasReplaceableRisk(first.Risks) {
		return first, nil
	}
	if firstErr != nil {
		log.Printf("ocr: local engine unavailable, trying system OCR: %v", firstErr)
	}
	second, secondErr := run("winrt", system)
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	if errors.Is(secondErr, context.Canceled) || errors.Is(secondErr, context.DeadlineExceeded) {
		return Text{}, secondErr
	}
	if firstErr != nil {
		if secondErr != nil {
			return Text{}, fmt.Errorf("ocr: both engines failed: %w", errors.Join(firstErr, secondErr))
		}
		if strings.TrimSpace(second.Text) == "" {
			return Text{}, fmt.Errorf("ocr: local engine failed and system found no text: %w", firstErr)
		}
		second.RetryCount, second.Selection = 1, "local-unavailable"
		return second, nil
	}
	first.RetryCount = 1
	if secondErr != nil {
		if strings.TrimSpace(first.Text) == "" {
			return Text{}, fmt.Errorf("ocr: local found no text and system failed: %w", secondErr)
		}
		first.Selection = "system-unavailable"
		return first, nil
	}
	if strings.TrimSpace(first.Text) == "" {
		second.RetryCount, second.Selection = 1, "local-empty"
		return second, nil
	}
	if preferSystem(first, second) {
		second.RetryCount, second.Selection = 1, "system-covers-uncertain-text"
		return second, nil
	}
	first.Selection = "kept-local"
	return first, nil
}
func runEngine(ctx context.Context, img image.Image, lang string, engine recognizeFunc) (Text, error) {
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	t, err := engine(ctx, img, lang)
	if cancel := ctx.Err(); cancel != nil {
		return t, cancel
	}
	if err != nil {
		return t, err
	}
	start := time.Now()
	result, err := finish(t)
	if err != nil {
		result = t
	}
	result.timings.LayoutUS = time.Since(start).Microseconds()
	return result, err
}

// Unknown system scores are never treated as perfect confidence. Replacement
// requires geometric coverage and preservation of the local engine's trusted
// text, plus a reason beyond small glyphs alone. This is a conservative policy,
// not a correctness score or a promise to detect every missing region.
func preferSystem(first, second Text) bool {
	if strings.TrimSpace(second.Text) == "" {
		return false
	}
	for _, risk := range second.Risks {
		if risk.Reason == "suspicious-character" || risk.Reason == "invalid-geometry" || risk.Reason == "empty-line" {
			return false
		}
	}
	if !hasReplaceableRisk(first.Risks) {
		return false
	}
	for _, line := range first.Lines {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		covered := false
		var matching strings.Builder
		for _, candidate := range second.Lines {
			if strings.TrimSpace(candidate.Text) == "" {
				continue
			}
			if overlap(line.Quad, candidate.Quad) >= .5 {
				covered = true
				matching.WriteString(candidate.Text)
			}
		}
		if !covered {
			return false
		}
		if line.HasScore && line.Score >= .85 && !strings.Contains(compactText(matching.String()), compactText(line.Text)) {
			return false
		}
	}
	return true
}

// Use the same eligibility for scheduling and selection: geometry-only risks
// cannot justify replacement by an engine with unknown confidence.
func hasReplaceableRisk(risks []pipeline.Risk) bool {
	for _, risk := range risks {
		switch risk.Reason {
		case "low-line-confidence", "low-token-confidence", "suspicious-character", "empty-line":
			return true
		}
	}
	return false
}

func compactText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// overlap measures how much of the first box is covered, in original pixels.
func overlap(a, b pipeline.Quad) float64 {
	bounds := func(q pipeline.Quad) (float64, float64, float64, float64) {
		x0, y0, x1, y1 := q[0].X, q[0].Y, q[0].X, q[0].Y
		for _, p := range q {
			x0 = min(x0, p.X)
			y0 = min(y0, p.Y)
			x1 = max(x1, p.X)
			y1 = max(y1, p.Y)
		}
		return x0, y0, x1, y1
	}
	ax, ay, ar, ab := bounds(a)
	bx, by, br, bb := bounds(b)
	area := (ar - ax) * (ab - ay)
	if area <= 0 {
		return 0
	}
	return max(0, min(ar, br)-max(ax, bx)) * max(0, min(ab, bb)-max(ay, by)) / area
}
