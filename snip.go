package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"sotto/internal/capture"
	"sotto/internal/ocr"
)

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// Each display gets its own client coordinate space and DPI. A drag is bounded
// to that display; cross-display text can be captured in separate shots.
type snipManager struct {
	mu         sync.Mutex
	wins       []*mygo.Window
	task       *entryTask
	shownAt    time.Time
	isQuitting func() bool
}

func (s *snipManager) Show(task *entryTask) {
	s.Hide()
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		task.cancel()
		return
	}
	s.mu.Lock()
	s.task = task
	s.mu.Unlock()
	cursor := mygo.Screen.CursorScreenPoint()
	active := 0
	for i, d := range displays {
		s.mu.Lock()
		var win *mygo.Window
		if i < len(s.wins) {
			win = s.wins[i]
		}
		s.mu.Unlock()
		if win == nil {
			win = mygo.NewWindow(mygo.WindowOptions{
				Title: "Sotto 截图", URL: "/snip.html", Hidden: true, Frameless: true,
				Panel: true, SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
				Opacity: 0.35, BackgroundColor: "#10141c", Page: mygo.PageOptions{DevTools: mygo.DevToolsAuto},
			})
			w := win
			w.OnClose(func(e *mygo.CloseEvent) {
				if s.isQuitting != nil && s.isQuitting() {
					return
				}
				e.PreventDefault()
				s.Hide()
				taskToCancel := s.current()
				if taskToCancel != nil {
					taskToCancel.cancel()
				}
			})
			w.OnBlur(func() {
				task := s.current()
				s.mu.Lock()
				shownAt := s.shownAt
				s.mu.Unlock()
				if task == nil || time.Since(shownAt) < 500*time.Millisecond {
					return
				}
				// Focus can move to another display's overlay in the same shot.
				go func() {
					time.Sleep(150 * time.Millisecond)
					mygo.RunOnMain(func() {
						if s.current() != task {
							return
						}
						for _, other := range s.windows() {
							if other.IsFocused() {
								return
							}
						}
						s.Hide()
						task.cancel()
					})
				}()
			})
			s.mu.Lock()
			s.wins = append(s.wins, win)
			s.mu.Unlock()
		}
		win.SetBounds(d.Bounds)
		win.Page().LoadURL(fmt.Sprintf("/snip.html#t=%d", task.id))
		if cursor.X >= d.Bounds.X && cursor.X < d.Bounds.X+d.Bounds.Width && cursor.Y >= d.Bounds.Y && cursor.Y < d.Bounds.Y+d.Bounds.Height {
			active = i
		}
	}
	wins := s.windows()
	s.mu.Lock()
	s.shownAt = time.Now()
	s.mu.Unlock()
	for _, win := range wins[:len(displays)] {
		win.ShowInactive()
	}
	wins[active].Show()
}
func (s *snipManager) windows() []*mygo.Window {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*mygo.Window(nil), s.wins...)
}
func (s *snipManager) current() *entryTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.task
}
func (s *snipManager) Hide() {
	for _, w := range s.windows() {
		w.Hide()
	}
}

// take is called on the UI thread so a stale Done cannot hide a newer shot.
func (s *snipManager) take(id int64, win *mygo.Window) *entryTask {
	s.mu.Lock()
	t := s.task
	validWindow := false
	for _, w := range s.wins {
		if w == win {
			validWindow = true
		}
	}
	if t == nil || t.id != id || !validWindow {
		s.mu.Unlock()
		return nil
	}
	s.task = nil
	s.mu.Unlock()
	s.Hide()
	if !t.valid() {
		return nil
	}
	return t
}

type snipRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (a *application) snipDone(win *mygo.Window, sessionID int64, rect *snipRect) {
	var task *entryTask
	var hwnd uintptr
	var anchor *Anchor
	mygo.RunOnMain(func() {
		task = a.snip.take(sessionID, win)
		if task == nil {
			return
		}
		if rect == nil || rect.W < 8 || rect.H < 8 {
			task.cancel()
			task = nil
			return
		}
		hwnd = win.NativeHandle()
		x, y := win.Position()
		anchor = &Anchor{X: x + rect.X, Y: y + rect.Y + rect.H}
	})
	if task == nil {
		return
	}
	go func() {
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
		if !task.valid() {
			return
		}
		phase, message := "recognizing", "正在识别文字…"
		if err != nil {
			phase, message = "error", "截屏失败，请重新框选。"
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
