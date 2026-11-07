// Sotto is a translation tool that lives on hotkeys and a bubble: type
// to translate, translate a selection or a screenshot, and pin the
// bubble to keep it around.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"sotto/internal/ocr"
	"sotto/internal/selection"
)

func main() {
	if handleGrabCommand() {
		return
	}
	if handleOCRCommand() {
		return
	}
	// The first instance keeps the lock; a second one hands its command
	// line over and quits.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	mygo.Use(updater.New(updater.Options{
		Language:               "zh-Hans",
		Icon:                   iconPNG,
		DisableAutomaticChecks: true,
	}))
	// Gate checks until ready has loaded the app's preference: the plugin's
	// own persisted preference otherwise overrides DisableAutomaticChecks.
	if updater.AutomaticChecks() {
		updater.SetAutomaticChecks(false)
	}
	app := newApplication()
	mygo.Bind(
		ConfigSvc{app},
		TranslateSvc{app},
		BubbleSvc{app},
		HotkeySvc{},
		SnipSvc{app},
	)
	mygo.App.WhenReady(app.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// grabReport is what the --grab commands print, one JSON object.
type grabReport struct {
	Via     string `json:"via"`
	Elapsed string `json:"elapsed"`
	Text    string `json:"text"`
	Rect    []int  `json:"rect,omitempty"`
	Err     string `json:"err,omitempty"`
}

// handleOCRCommand runs the OCR test command:
//
//	sotto --ocr <png> [lang]
//
// It recognizes the image and prints the result as JSON.
func handleOCRCommand() bool {
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

// handleGrabCommand runs the selection test commands:
//
//	sotto --grab            the whole pipeline, UIA first
//	sotto --grab uia        only the UI Automation path
//	sotto --grab clipboard  only the simulated copy
//
// Each waits three seconds first, so the tester can switch to the
// window whose selection to grab — selection survives losing focus.
func handleGrabCommand() bool {
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
	switch path {
	case "uia":
		sel, err = selection.GrabUIA(ctx)
	case "clipboard":
		sel, err = selection.GrabClipboard(ctx)
	default:
		sel, err = selection.Grab(ctx)
	}

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
