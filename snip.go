package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sotto/internal/capture"
	"sotto/internal/ocr"
)

// All manager and native-view state belongs to the UI thread. Capture workers
// return their HWND leases through RunOnMain after GDI has finished.
type snipManager struct {
	screens    map[int64]*snipScreen
	task       *entryTask
	shownAt    time.Time
	isQuitting func() bool
	done       func(*mygo.Window, int64, *snipRect)
}

func (s *snipManager) Show(task *entryTask) {
	s.Hide()
	displays := mygo.Screen.Displays()
	s.prune(displays)
	if len(displays) == 0 {
		task.cancel()
		return
	}
	if s.screens == nil {
		s.screens = make(map[int64]*snipScreen)
	}
	s.task = task
	cursor := mygo.Screen.CursorScreenPoint()
	active := 0
	ordered := make([]*snipScreen, 0, len(displays))
	for i, d := range displays {
		screen := s.reusableScreen(d.ID)
		if screen == nil {
			screen = s.newScreen(d)
			s.screens[d.ID] = screen
		}
		screen.round = &snipRound{task: task}
		screen.win.SetBounds(d.Bounds)
		screen.win.Invalidate()
		ordered = append(ordered, screen)
		if cursor.X >= d.Bounds.X && cursor.X < d.Bounds.X+d.Bounds.Width && cursor.Y >= d.Bounds.Y && cursor.Y < d.Bounds.Y+d.Bounds.Height {
			active = i
		}
	}
	s.shownAt = time.Now()
	for _, screen := range ordered {
		screen.win.ShowInactive()
	}
	ordered[active].win.Show()
}

func (s *snipManager) reusableScreen(id int64) *snipScreen {
	screen := s.screens[id]
	if screen != nil && (screen.used || screen.captures != 0) {
		// Hidden surfaces can retain the last selection: Invalidate is async,
		// and hidden windows need not present a clean frame before Show.
		// On Windows a new surface stays cloaked until its first frame. Keep
		// any borrowed HWND hidden and alive until its capture returns.
		delete(s.screens, id)
		screen.retire()
		return nil
	}
	return screen
}

func (s *snipManager) newScreen(display mygo.Display) *snipScreen {
	screen := &snipScreen{display: display}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 截图", Hidden: true, Frameless: true,
		Panel: true, SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
		Transparent: true, BackgroundColor: snipBackground,
		Content: ui.View(screen.view),
	})
	screen.win, screen.dispose = w, w.Destroy
	screen.submit = func(round *snipRound, rect *snipRect) {
		if s.task == round.task && s.done != nil {
			s.done(w, round.task.id, rect)
		}
	}
	w.OnClose(func(e *mygo.CloseEvent) {
		if s.isQuitting != nil && s.isQuitting() {
			return
		}
		e.PreventDefault()
		s.cancelCurrent()
	})
	w.OnClosed(func() {
		screen.disposed = true
		if s.screens[display.ID] == screen {
			delete(s.screens, display.ID)
		}
	})
	w.OnBlur(func() {
		task := s.task
		if task == nil || time.Since(s.shownAt) < 500*time.Millisecond {
			return
		}
		// Focus can transfer between displays within the same shot.
		go func() {
			time.Sleep(150 * time.Millisecond)
			mygo.RunOnMain(func() {
				if s.task != task {
					return
				}
				for _, other := range s.screens {
					if other.win.IsFocused() {
						return
					}
				}
				s.cancelCurrent()
			})
		}()
	})
	return screen
}

func (s *snipManager) Hide() {
	s.task = nil
	for _, screen := range s.screens {
		screen.round = nil
		screen.win.Hide()
		screen.win.Invalidate()
	}
}

func (s *snipManager) cancelCurrent() {
	task := s.task
	s.Hide()
	if task != nil {
		task.cancel()
	}
}

func (s *snipManager) refreshDisplays() {
	s.cancelCurrent()
	s.prune(mygo.Screen.Displays())
}

// Call after hiding the overlays: a changed display invalidates the current
// gesture, and an outstanding capture keeps its original HWND until completion.
func (s *snipManager) prune(displays []mygo.Display) {
	current := make(map[int64]mygo.Display, len(displays))
	for _, d := range displays {
		current[d.ID] = d
	}
	for id, screen := range s.screens {
		d, present := current[id]
		if !present || d.Bounds != screen.display.Bounds || d.ScaleFactor != screen.display.ScaleFactor {
			delete(s.screens, id)
			screen.retire()
		}
	}
}

// take only consumes a current task from one of its live screen windows. The
// caller hides the overlays after acceptance; stale events never hide a new shot.
func (s *snipManager) take(id int64, win *mygo.Window) (*entryTask, *snipScreen) {
	task := s.task
	if task == nil || task.id != id || !task.valid() {
		return nil, nil
	}
	for _, screen := range s.screens {
		if screen.win == win && !screen.retired && !screen.disposed {
			s.task = nil
			return task, screen
		}
	}
	return nil, nil
}

func (a *application) snipDone(win *mygo.Window, sessionID int64, rect *snipRect) {
	var task *entryTask
	var screen *snipScreen
	var hwnd uintptr
	var anchor *Anchor
	mygo.RunOnMain(func() {
		task, screen = a.snip.take(sessionID, win)
		if task == nil {
			return
		}
		anchor = snipCaptureAnchor(win.ContentBounds(), rect)
		if anchor == nil {
			a.snip.Hide()
			task.cancel()
			task = nil
			return
		}
		screen.captures++
		screen.captureTask = task
		hwnd = win.NativeHandle()
		a.snip.Hide()
	})
	if task == nil {
		return
	}
	go func() {
		release := sync.OnceFunc(func() { mygo.RunOnMain(screen.releaseCapture) })
		defer release()
		timer := time.NewTimer(120 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-task.ctx.Done():
			return
		case <-timer.C:
		}
		if !task.valid() {
			return
		}
		img, err := capture.GrabWindow(hwnd, capture.Rect{X: rect.X, Y: rect.Y, W: rect.W, H: rect.H})
		release()
		if !task.valid() {
			return
		}
		phase, message := "recognizing", "正在识别文字…"
		if err != nil {
			phase, message = "error", capture.UserMessage(err)
			log.Printf("capture: %v", err)
		}
		shown := a.bubbles.ShowTask(a.settings(), "ocr", "", anchor, phase, message, task.valid, task.cancel)
		if shown.Win == nil || err != nil || !task.valid() {
			return
		}
		ctx, cancel := context.WithTimeout(task.ctx, 30*time.Second)
		defer cancel()
		result, err := ocr.Recognize(ctx, img)
		if !task.valid() {
			return
		}
		message = ""
		if err != nil {
			message = ocr.UserMessage(err)
			log.Printf("ocr: %v", err)
		} else if result.Text == "" {
			message = "未识别到文字，请框选完整文字后重试。"
		}
		if !a.bubbles.completeCapture(a.settings(), shown, result.Text, message) {
			return
		}
		if message == "" && task.valid() {
			a.runTranslation(shown.Win, RunReq{ReqID: shown.ReqID, SessionID: shown.ReqID, Revision: 0, Source: "ocr", Text: result.Text})
		}
	}()
}
