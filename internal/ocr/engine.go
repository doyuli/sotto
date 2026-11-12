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
	// Unsupported explicit languages cannot be recognized by the fixed local
	// model and dictionary. Skip it rather than trusting a wrong-script result.
	if !localLanguage(lang) {
		t, err := run("winrt", system)
		if err != nil {
			return Text{}, err
		}
		return t, nil
	}
	first, firstErr := run("ppocr-v6-small", local)
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	if errors.Is(firstErr, context.Canceled) || errors.Is(firstErr, context.DeadlineExceeded) {
		return Text{}, firstErr
	}
	// Confidence risks remain diagnostic because another engine call would
	// delay usable text without a reliable way to establish a better result.
	if firstErr == nil && strings.TrimSpace(first.Text) != "" {
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
	if secondErr != nil {
		return Text{}, fmt.Errorf("ocr: local found no text and system failed: %w", secondErr)
	}
	second.RetryCount, second.Selection = 1, "local-empty"
	return second, nil
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
