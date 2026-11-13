package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"os"
	"time"

	"sotto/internal/ocr"
	"sotto/internal/selection"
)

// grabReport is what the --grab commands print, one JSON object.
type grabReport struct {
	Via     string `json:"via"`
	Elapsed string `json:"elapsed"`
	Text    string `json:"text"`
	Rect    []int  `json:"rect,omitempty"`
	Err     string `json:"err,omitempty"`
}

// HandleOCRCommand runs the OCR test command:
//
//	sotto --ocr <png> [lang]
//
// It recognizes the image and prints the result as JSON.
func HandleOCRCommand() bool {
	args := os.Args[1:]
	if len(args) < 2 || args[0] != "--ocr" {
		return false
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		log.Fatalf("not a png: %v", err)
	}
	lang := ""
	if len(args) > 2 {
		lang = args[2]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := time.Now()
	var text ocr.Text
	if lang == "" {
		text, err = ocr.Recognize(ctx, img)
	} else {
		text, err = ocr.RecognizeWith(ctx, img, lang)
	}
	report := struct {
		ocr.Text
		Err string `json:"err,omitempty"`
	}{Text: text}
	report.Elapsed = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		report.Err = err.Error()
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		log.Fatal(err)
	}
	return true
}

// HandleGrabCommand runs the selection test commands:
//
//	sotto --grab            the whole pipeline, UIA first
//	sotto --grab uia        only the UI Automation path
//	sotto --grab clipboard  only the simulated copy
//
// Each waits three seconds first, so the tester can switch to the
// window whose selection to grab — selection survives losing focus.
func HandleGrabCommand() bool {
	args := os.Args[1:]
	if len(args) == 0 || args[0] != "--grab" {
		return false
	}
	path := ""
	if len(args) > 1 {
		path = args[1]
	}
	fmt.Fprintln(os.Stderr, "3 秒后抓取：切到目标窗口（选中要翻译的文字）…")
	time.Sleep(3 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	var sel selection.Selection
	var err error
	sel, err = runGrabCommand(ctx, path)

	report := grabReport{Via: sel.Via, Elapsed: time.Since(start).Round(time.Millisecond).String(), Text: sel.Text}
	if sel.HasRect {
		report.Rect = []int{sel.Rect.X, sel.Rect.Y, sel.Rect.W, sel.Rect.H}
	}
	if err != nil {
		report.Err = err.Error()
	}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(report); err != nil {
		log.Fatal(err)
	}
	return true
}

func grabCommandPath(ctx context.Context, path string) (selection.Selection, error) {
	switch path {
	case "uia", "ax":
		return selection.GrabUIA(ctx)
	case "clipboard":
		return selection.GrabClipboard(ctx)
	default:
		return selection.Grab(ctx)
	}
}
