package snip

import (
	"context"
	"image"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/capture"
	"sotto/internal/platform"
)

// Capture keeps the source window alive until Grab completes. Grab is called
// exactly once off the UI thread; it releases the native lease on every path.
type Capture struct {
	Task      Task
	Anchor    mygo.Point
	handle    uintptr
	displayID int64
	rect      capture.Rect
	release   func()
}

func (s *Manager) finish(win *mygo.Window, sessionID int64, rect *Rect) {
	task, screen := s.take(sessionID, win)
	if task == nil {
		return
	}
	content := win.ContentBounds()
	anchor := snipCaptureAnchor(content, rect)
	if anchor == nil {
		s.Hide()
		task.Cancel()
		return
	}
	screen.captures++
	screen.captureTask = task
	hwnd := win.NativeHandle()
	s.Hide()

	shot := &Capture{Task: task, Anchor: *anchor, handle: hwnd, displayID: screen.display.ID, rect: captureRect(content, screen.display.Bounds, rect), release: sync.OnceFunc(func() { mygo.RunOnMain(screen.releaseCapture) })}
	if s.Done == nil {
		shot.release()
		task.Cancel()
		return
	}
	s.Done(shot)
}

func (c *Capture) Grab() (image.Image, error) {
	defer c.release()
	timer := time.NewTimer(platform.CaptureDelay())
	defer timer.Stop()
	select {
	case <-c.Task.Context().Done():
		return nil, c.Task.Context().Err()
	case <-timer.C:
	}
	if !c.Task.Valid() {
		return nil, context.Canceled
	}
	return capture.GrabDisplayWindow(c.Task.Context(), c.handle, c.displayID, c.rect)
}
