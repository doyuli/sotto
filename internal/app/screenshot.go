package app

import (
	"context"
	"log"
	"time"

	"sotto/internal/capture"
	"sotto/internal/ocr"
	"sotto/internal/translate"
	"sotto/internal/ui/bubble"
	"sotto/internal/ui/snip"
)

func (a *application) snipFailed(task snip.Task, err error) {
	log.Printf("capture: %v", err)
	if entry, ok := task.(*entryTask); ok && entry.simple {
		a.showSimpleEntryError(entry, entry.source(), capture.SnapshotUserMessage(err))
		return
	}
	go a.bubbles.ShowTask(a.settings(), "ocr", "", nil, "error", capture.SnapshotUserMessage(err), task.Valid, task.Cancel)
}

// snipDone crops the snapshot and starts recognition after the overlays close.
func (a *application) snipDone(shot *snip.Capture) {
	task := shot.Task
	if entry, ok := task.(*entryTask); ok && entry.simple {
		a.startSimpleJob(entry, shot.Region, func(ctx context.Context, progress func(string)) translate.Result {
			return a.simpleScreenshot(ctx, shot, progress)
		})
		return
	}
	anchor := &bubble.Anchor{X: shot.Region.X, Y: shot.Region.Y + shot.Region.Height}
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
		if message == "" {
			if err := translate.ValidateInput(result.Text); err != nil {
				result.Text = ""
				message = translate.UserMessage(err)
			}
		}
		if !a.bubbles.CompleteCapture(a.settings(), shown, result.Text, message) {
			return
		}
		if message == "" && task.Valid() {
			a.runTranslation(shown.Win, bubble.RunReq{ReqID: shown.ReqID, SessionID: shown.ReqID, Revision: 0, Source: "ocr", Text: result.Text})
		}
	}()
}
