package main

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const snipBackground = "#10141c1a"

type snipRound struct {
	task      *entryTask
	selection snipSelection
}

// The UI thread owns the screen, its round and its capture leases. Retiring a
// screen hides it immediately but waits for GrabWindow before destroying HWND.
type snipScreen struct {
	display     mygo.Display
	win         *mygo.Window
	round       *snipRound
	submit      func(*snipRound, *snipRect)
	dispose     func()
	captures    int
	captureTask *entryTask
	used        bool
	retired     bool
	disposed    bool
}

func (s *snipScreen) retire() {
	s.retired, s.round = true, nil
	if s.captureTask != nil {
		s.captureTask.cancel()
	}
	s.destroyIfUnused()
}

func (s *snipScreen) releaseCapture() {
	s.captures--
	if s.captures == 0 {
		s.captureTask = nil
	}
	s.destroyIfUnused()
}

func (s *snipScreen) destroyIfUnused() {
	if s.retired && s.captures == 0 && !s.disposed {
		s.disposed = true
		if s.dispose != nil {
			s.dispose()
		}
	}
}

func (s *snipScreen) view(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	round := s.round
	if round == nil {
		ui.Box(c).Fill().Background(ui.Hex(snipBackground))
		return
	}
	round.selection.width, round.selection.height = c.Size()
	ui.Box(c.Key(round.task.id)).Fill().Background(ui.Hex(snipBackground)).
		Cursor(ui.CursorCrosshair).Focusable().AutoFocus().Label("截图框选").
		HandleInput(func(ev ui.InputEvent) bool { return s.input(round, ev) }).
		Draw(func(p *ui.Painter, r ui.Rect) { drawSnip(p, r, &round.selection) })
}

func (s *snipScreen) input(round *snipRound, ev ui.InputEvent) bool {
	// A removed element may still deliver its canceled release. Its closure
	// belongs to the old round, not the replacement now shown in this window.
	if s.round != round || round.selection.submitted || !round.task.valid() {
		return false
	}
	var rect *snipRect
	var finished bool
	switch ev.Kind {
	case ui.InputPointerDown:
		switch ev.Button {
		case 0:
			if round.selection.begin(ev.X, ev.Y) {
				s.used = true
				return true
			}
		case 1:
			finished = round.selection.cancel()
		}
	case ui.InputPointerMove:
		return round.selection.move(ev.X, ev.Y)
	case ui.InputPointerUp:
		if ev.Button == 0 {
			rect, finished = round.selection.finish(ev.X, ev.Y, ev.Canceled)
		}
	case ui.InputKeyDown:
		if ev.Key == ui.KeyEscape {
			finished = round.selection.cancel()
		}
	}
	if finished && s.submit != nil {
		s.submit(round, rect)
	}
	return finished
}

func drawSnip(p *ui.Painter, area ui.Rect, s *snipSelection) {
	if !s.dragging {
		return
	}
	x, y, w, h := s.bounds()
	box := ui.Rect{X: area.X + x, Y: area.Y + y, W: w, H: h}
	p.Fill(box, ui.Hex("#4cc2ff26"), 0)
	p.Stroke(box, ui.Hex("#4cc2ff"), 0, 1)
	label := ui.Span{Text: fmt.Sprintf("%d × %d DIPs", roundSnip(w), roundSnip(h)), Size: 11, Color: ui.Hex("#ffffff")}
	lw, lh := p.MeasureText(0, label)
	lx := max(area.X, min(box.X, area.X+area.W-lw-14))
	ly := max(area.Y, min(box.Y-lh-6, area.Y+area.H-lh-2))
	p.Fill(ui.Rect{X: lx, Y: ly, W: lw + 14, H: lh + 2}, ui.Hex("#2f7fe8"), 4)
	p.RichText(lx+7, ly+1, 0, label)
}
