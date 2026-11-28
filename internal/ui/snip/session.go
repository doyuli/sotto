package snip

import (
	"context"
	"fmt"
	"image"
	"log"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/capture"
	"sotto/internal/platform"
)

// Task ties a selection to the app entry that requested it. Late input and
// captures are rejected after another entry or workspace revokes ownership.
type Task interface {
	ID() int64
	Context() context.Context
	Valid() bool
	Cancel()
}

// Manager owns the pending snapshot and selection on the UI thread.
type Manager struct {
	screens        map[int64]*snipScreen
	task           Task
	releaseEscape  func()
	shownAt        time.Time
	IsQuitting     func() bool
	Done           func(*Capture)
	Failed         func(Task, error)
	BeginCapture   func() func()
	RegisterEscape func(func()) (func(), error)
	SuspendResults func() func()
	resumeResults  func()
}

// Keep each batch's admission until the UI has taken or dropped its pixels.
var snapshotAdmission = make(chan struct{}, 1)

func (s *Manager) Show(task Task) {
	s.Hide()
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		task.Cancel()
		return
	}
	s.beginSnapshot(task)
	if s.SuspendResults != nil {
		s.resumeResults = s.SuspendResults()
	}
	source := platform.ScreenshotSource()
	cursorDisplay := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	ids := make([]int64, 0, len(displays))
	for _, display := range displays {
		ids = append(ids, display.ID)
	}
	// Under a tight budget, let the display the user was on go first.
	if index := slices.Index(ids, cursorDisplay.ID); index > 0 {
		copy(ids[1:index+1], ids[:index])
		ids[0] = cursorDisplay.ID
	}
	go func() {
		if s.BeginCapture != nil {
			defer s.BeginCapture()()
		}
		ctx := task.Context()
		select {
		case snapshotAdmission <- struct{}{}:
			defer func() { <-snapshotAdmission }()
		case <-ctx.Done():
			return
		}
		frames, err := snapshotDisplays(ctx, displays, ids)
		mygo.RunOnMain(func() {
			if s.acceptSnapshot(task, source, platform.ScreenshotSource()) {
				s.snapshotReady(task, displays, frames, err)
			}
		})
	}()
}

func (s *Manager) beginSnapshot(task Task) {
	s.task = task
	if s.RegisterEscape == nil {
		return
	}
	// Only the pending snapshot needs a global cancel key. The overlay
	// handles Escape itself once it receives focus.
	release, err := s.RegisterEscape(func() {
		if s.task == task && s.releaseEscape != nil {
			s.cancelCurrent()
		}
	})
	if err != nil {
		log.Printf("screenshot: register Escape while snapshot is pending: %v", err)
		return
	}
	s.releaseEscape = release
}

func (s *Manager) stopPendingEscape() {
	if release := s.releaseEscape; release != nil {
		s.releaseEscape = nil
		release()
	}
}

func (s *Manager) acceptSnapshot(task Task, source, currentSource uintptr) bool {
	if s.task != task || !task.Valid() {
		return false
	}
	if source != 0 && source != currentSource {
		s.cancelCurrent()
		return false
	}
	s.stopPendingEscape()
	return true
}

func (s *Manager) snapshotReady(task Task, displays []mygo.Display, frames map[int64]*snapshotFrame, err error) {
	if !slices.Equal(displays, mygo.Screen.Displays()) {
		s.cancelCurrent()
		return
	}
	if len(frames) > 0 {
		if err != nil {
			log.Printf("screenshot: some displays unavailable: %v", err)
		}
		err = s.showFrames(task, displays, frames)
	}
	if err != nil {
		s.Hide()
		if s.Failed != nil {
			s.Failed(task, err)
		} else {
			task.Cancel()
		}
	}
}

func snapshotDisplays(ctx context.Context, displays []mygo.Display, ids []int64) (map[int64]*snapshotFrame, error) {
	snapshots, err := capture.SnapshotDisplays(ctx, ids)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	bounds := make(map[int64]mygo.Rectangle, len(displays))
	for _, display := range displays {
		bounds[display.ID] = display.Bounds
	}
	frames := make(map[int64]*snapshotFrame, len(snapshots))
	for _, snapshot := range snapshots {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		img := snapshot.Image
		frames[snapshot.ID] = &snapshotFrame{image: img, bitmap: ui.NewBitmap(img), display: bounds[snapshot.ID]}
	}
	return frames, err
}

func (s *Manager) showFrames(task Task, displays []mygo.Display, frames map[int64]*snapshotFrame) error {
	s.screens = make(map[int64]*snipScreen, len(displays))
	cursor := mygo.Screen.CursorScreenPoint()
	active := 0
	ordered := make([]*mygo.Window, 0, len(displays))
	for _, d := range displays {
		frame := frames[d.ID]
		if frame == nil {
			continue
		}
		screen, err := s.newScreen(d, &snipRound{task: task, frame: frame, content: d.Bounds})
		if err != nil {
			return fmt.Errorf("configure screenshot overlay for display %d: %w", d.ID, err)
		}
		s.screens[d.ID] = screen
		ordered = append(ordered, screen.win)
		if image.Pt(cursor.X, cursor.Y).In(image.Rect(d.Bounds.X, d.Bounds.Y, d.Bounds.X+d.Bounds.Width, d.Bounds.Y+d.Bounds.Height)) {
			active = len(ordered) - 1
		}
	}
	s.shownAt = time.Now()
	for _, win := range ordered {
		win.ShowInactive()
	}
	ordered[active].Show()
	return nil
}

func (s *Manager) newScreen(display mygo.Display, round *snipRound) (*snipScreen, error) {
	screen := &snipScreen{round: round}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 截图", Hidden: true, Frameless: true,
		Panel: true, SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
		Transparent: true, BackgroundColor: snipBackground,
		X: display.Bounds.X, Y: display.Bounds.Y,
		Width: display.Bounds.Width, Height: display.Bounds.Height,
		Content: ui.View(screen.view),
	})
	screen.win = w
	if err := platform.ConfigureScreenshotWindow(w, display.Bounds); err != nil {
		w.Destroy()
		return nil, err
	}
	updateContent := func() {
		round := screen.round
		if round == nil {
			return
		}
		if content := w.ContentBounds(); content != round.content {
			round.content = content
			w.Invalidate()
		}
	}
	updateContent()
	w.OnShow(updateContent)
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
		screen.clearRound()
		if s.screens[display.ID] == screen {
			delete(s.screens, display.ID)
		}
	})
	task := round.task
	w.OnBlur(func() {
		if s.task != task || time.Since(s.shownAt) < 500*time.Millisecond {
			return
		}
		// Focus can transfer between displays within the same shot.
		time.AfterFunc(150*time.Millisecond, func() {
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
		})
	})
	return screen, nil
}

func (s *Manager) Hide() {
	s.task = nil
	s.stopPendingEscape()
	screens := s.screens
	s.screens = nil
	for _, screen := range screens {
		screen.clearRound()
		w := screen.win
		w.Hide()
		// Update queues destruction beyond the native input callback. Closed
		// windows have no content to redraw after Destroy returns.
		w.Update(w.Destroy)
	}
	if resume := s.resumeResults; resume != nil {
		s.resumeResults = nil
		resume()
	}
}

func (s *Manager) cancelCurrent() {
	task := s.task
	s.Hide()
	if task != nil {
		task.Cancel()
	}
}

func (s *Manager) RefreshDisplays() { s.cancelCurrent() }

// take only consumes a current task from one of its live screen windows. The
// caller hides the overlays after acceptance; stale events never hide a new shot.
func (s *Manager) take(id int64, win *mygo.Window) (Task, *snipScreen) {
	task := s.task
	if task == nil || task.ID() != id || !task.Valid() {
		return nil, nil
	}
	for _, screen := range s.screens {
		if screen.win == win && screen.round != nil {
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
