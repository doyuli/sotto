package snip

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Task ties a selection to the app entry that requested it. Late input and
// captures are rejected after another entry or workspace revokes ownership.
type Task interface {
	ID() int64
	Context() context.Context
	Valid() bool
	Cancel()
}

// All manager and native-view state belongs to the UI thread. Capture workers
// return their HWND leases through RunOnMain after GDI has finished.
type Manager struct {
	screens    map[int64]*snipScreen
	task       Task
	shownAt    time.Time
	IsQuitting func() bool
	Done       func(*Capture)
}

func (s *Manager) Show(task Task) {
	s.Hide()
	displays := mygo.Screen.Displays()
	s.prune(displays)
	if len(displays) == 0 {
		task.Cancel()
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

func (s *Manager) reusableScreen(id int64) *snipScreen {
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

func (s *Manager) newScreen(display mygo.Display) *snipScreen {
	screen := &snipScreen{display: display}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 截图", Hidden: true, Frameless: true,
		Panel: true, SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
		Transparent: true, BackgroundColor: snipBackground,
		Content: ui.View(screen.view),
	})
	screen.win, screen.dispose = w, w.Destroy
	screen.submit = func(round *snipRound, rect *Rect) {
		if s.task == round.task {
			s.finish(w, round.task.ID(), rect)
		}
	}
	w.OnClose(func(e *mygo.CloseEvent) {
		if s.IsQuitting != nil && s.IsQuitting() {
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

func (s *Manager) Hide() {
	s.task = nil
	for _, screen := range s.screens {
		screen.round = nil
		screen.win.Hide()
		screen.win.Invalidate()
	}
}

func (s *Manager) cancelCurrent() {
	task := s.task
	s.Hide()
	if task != nil {
		task.Cancel()
	}
}

func (s *Manager) RefreshDisplays() {
	s.cancelCurrent()
	s.prune(mygo.Screen.Displays())
}

// Call after hiding the overlays: a changed display invalidates the current
// gesture, and an outstanding capture keeps its original HWND until completion.
func (s *Manager) prune(displays []mygo.Display) {
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
func (s *Manager) take(id int64, win *mygo.Window) (Task, *snipScreen) {
	task := s.task
	if task == nil || task.ID() != id || !task.Valid() {
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

// HideStale cancels only the overlay belonging to an obsolete entry.
func (s *Manager) HideStale() {
	if s.task != nil && !s.task.Valid() {
		s.cancelCurrent()
	}
}
