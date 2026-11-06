// Package ocr recognizes text in images. The engine chain starts with a
// local PP-OCRv3 model run through the ONNX Runtime C API directly — no
// cgo — and falls back to Windows' own Windows.Media.Ocr, reached through
// a resident Windows PowerShell 5.1 subprocess: WinRT projection does not
// work in pwsh, and a cold start costs a second or two, so that process
// stays alive and answers line-JSON requests on stdin.
package ocr

import (
	"context"
	"image"
)

// Line is one recognized line, with its box in image pixels.
type Line struct {
	Text string `json:"text"`
	Box  [4]int `json:"box"` // x, y, w, h
}

// Text is what one recognition found.
type Text struct {
	Lines   []Line `json:"lines"`
	Text    string `json:"text"`
	Engine  string `json:"engine"`
	Elapsed string `json:"elapsed,omitempty"`
}

// Recognize runs the system OCR over img.
var admission = make(chan struct{}, 1)

func Recognize(ctx context.Context, img image.Image) (Text, error) {
	select {
	case admission <- struct{}{}:
	case <-ctx.Done():
		return Text{}, ctx.Err()
	}
	defer func() { <-admission }()
	if err := ctx.Err(); err != nil {
		return Text{}, err
	}
	t, err := recognize(ctx, img)
	return finish(t), err
}

// RecognizeWith runs OCR with an explicit UI language tag ("zh-Hans");
// empty falls back to the user's profile languages.
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
	t, err := recognizeWith(ctx, img, lang)
	return finish(t), err
}

// finish applies the one post-processing pass every engine shares, so a
// caller never sees OCR control characters or CJK word-spacing.
func finish(t Text) Text {
	t.Text = cleanText(t.Text)
	for i := range t.Lines {
		t.Lines[i].Text = cleanText(t.Lines[i].Text)
	}
	return t
}

// AvailableLanguages lists the language packs the installed OCR can
// recognize; empty when the engine is unusable.
func AvailableLanguages(ctx context.Context) []string {
	return availableLanguages(ctx)
}
