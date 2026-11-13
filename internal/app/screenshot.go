package app

import (
	"context"
	"log"
	"time"

	"sotto/internal/capture"
	"sotto/internal/ocr"
	"sotto/internal/ui/bubble"
	"sotto/internal/ui/snip"
)

// snipDone starts recognition after snip has hidden the overlays and leased
// the source window. The capture releases its lease before OCR or networking.
func (a *application) snipDone(shot *snip.Capture) {
	task := shot.Task
	anchor := &bubble.Anchor{X: shot.Anchor.X, Y: shot.Anchor.Y}
	go func() {
		img, err := shot.Grab()
		if !task.Valid() {
			return
		}
		phase, message := "recognizing", "正在识别文字…"
		if err != nil {
			phase, message = "error", capture.UserMessage(err)
			log.Printf("capture: %v", err)
		}
		shown := a.bubbles.ShowTask(a.settings(), "ocr", "", anchor, phase, message, task.Valid, task.Cancel)
		if shown.Win == nil || err != nil || !task.Valid() {
			return
		}
		ctx, cancel := context.WithTimeout(task.Context(), 30*time.Second)
		defer cancel()
		result, err := ocr.Recognize(ctx, img)
		if !task.Valid() {
			return
		}
		message = ""
		if err != nil {
			message = ocr.UserMessage(err)
			log.Printf("ocr: %v", err)
		} else if result.Text == "" {
			message = "未识别到文字，请框选完整文字后重试。"
		}
		if !a.bubbles.CompleteCapture(a.settings(), shown, result.Text, message) {
			return
		}
		if message == "" && task.Valid() {
			a.runTranslation(shown.Win, bubble.RunReq{ReqID: shown.ReqID, SessionID: shown.ReqID, Revision: 0, Source: "ocr", Text: result.Text})
		}
	}()
}
