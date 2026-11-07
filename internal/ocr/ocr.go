// Package ocr recognizes screenshots locally with PP-OCRv6 small and a bounded
// Windows.Media.Ocr fallback. Both engines share one layout and text output.
package ocr

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	"sotto/internal/ocr/pipeline"
)

type Text struct {
	Lines       []pipeline.Line      `json:"lines"`
	Blocks      []pipeline.Block     `json:"blocks"`
	Text        string               `json:"text"`
	Engine      string               `json:"engine"`
	Elapsed     string               `json:"elapsed,omitempty"`
	Diagnostics pipeline.Diagnostics `json:"diagnostics"`
	Risks       []pipeline.Risk      `json:"risks,omitempty"`
	RetryCount  int                  `json:"retryCount"`
	Selection   string               `json:"selection,omitempty"`
}

var admission = make(chan struct{}, 1)

func Recognize(ctx context.Context, img image.Image) (Text, error) {
	return RecognizeWith(ctx, img, "")
}

// RecognizeWith honors an explicit BCP-47 language tag. The gate also makes
// cancellation responsive while an earlier native inference is still running.
func RecognizeWith(ctx context.Context, img image.Image, lang string) (Text, error) {
	select {
	case admission <- struct{}{}:
	case <-ctx.Done():
		return Text{}, ctx.Err()
	}
	defer func() { <-admission }()
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	if err := validateImage(img); err != nil {
		return Text{}, err
	}
	start := time.Now()
	result, err := recognizeWith(ctx, img, lang)
	if cancel := ctx.Err(); cancel != nil {
		return Text{}, cancel
	}
	result.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return result, err
}
func validateImage(img image.Image) error {
	if img == nil || img.Bounds().Empty() {
		return fmt.Errorf("ocr: empty image")
	}
	b := img.Bounds()
	c := pipeline.DefaultConfig()
	if b.Dx() > c.MaxImageSide || b.Dy() > c.MaxImageSide || b.Dx() > c.MaxImagePixels/b.Dy() {
		return fmt.Errorf("%w: image dimensions", pipeline.ErrLimit)
	}
	return nil
}

// Raw lines and scores remain available for diagnostics; only the copy passed
// to layout is cleaned. Text is always regenerated from that same layout.
func finish(t Text) (Text, error) {
	t.Risks = pipeline.Assess(pipeline.Result{Lines: t.Lines, Diagnostics: t.Diagnostics}).Risks
	lines := append([]pipeline.Line(nil), t.Lines...)
	for i := range lines {
		lines[i].Text = strings.ReplaceAll(cleanText(lines[i].Text), "\n", " ")
	}
	doc, err := pipeline.Layout(lines, pipeline.LayoutOptions{})
	if err != nil {
		return Text{}, fmt.Errorf("ocr layout: %w", err)
	}
	t.Text, t.Blocks = doc.Text, doc.Blocks
	return t, nil
}
func UserMessage(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "文字识别已取消。"
	case errors.Is(err, context.DeadlineExceeded):
		return "文字识别超时，请缩小框选范围后重试。"
	case errors.Is(err, pipeline.ErrLimit):
		return "截图内容过大或文字行过长，请缩小框选范围后重试。"
	case err != nil && strings.Contains(err.Error(), "language pack"):
		return "系统 OCR 语言包不可用，请在 Windows 设置中安装所需语言的文字识别功能。"
	default:
		return "文字识别失败，请重新框选或直接输入。"
	}
}
