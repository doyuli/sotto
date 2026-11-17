// Package overlay presents independent, text-only translation results.
package overlay

import (
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/platform"
)

// Manager and its windows belong to the main thread.
type Manager struct {
	windows   []*Result
	suspended bool
}

// Result is a floating translation that can be completed in place.
// Its methods run on the main thread.
type Result struct {
	window    *mygo.Window
	source    mygo.Rectangle
	work      mygo.Rectangle
	workspace uint64
	view      resultView
	applied   size
	ready     bool
	presented bool
	moved     bool
	onCancel  func()
}

func (m *Manager) Show(text, message string, source mygo.Rectangle, workspace uint64) {
	m.show(resultText(text, message), source, workspace, nil)
}

// ShowPending creates a cancellable window with either a loading state or
// the first cumulative snapshot from the provider.
func (m *Manager) ShowPending(text string, source mygo.Rectangle, workspace uint64, onCancel func()) *Result {
	return m.show(text, source, workspace, onCancel)
}

func (m *Manager) show(text string, source mygo.Rectangle, workspace uint64, onCancel func()) *Result {
	work := workArea(source)
	p := &Result{source: source, work: work, workspace: workspace, onCancel: onCancel}
	p.view = resultView{
		text: text, pending: onCancel != nil, limit: limits(work),
		cursor: mygo.Screen.CursorScreenPoint,
		position: func() mygo.Point {
			r := p.window.Bounds()
			return mygo.Point{X: r.X, Y: r.Y}
		},
		move: func(point mygo.Point) {
			p.moved = true
			r := p.window.Bounds()
			r.X, r.Y = point.X, point.Y
			p.setWorkArea(workArea(r))
			r = fit(p.work, r)
			p.window.SetPosition(r.X, r.Y)
		},
		focus: func() {
			if !p.window.IsFocused() {
				p.window.Focus()
			}
		},
		dismiss: func() {
			p.cancelPending()
			p.window.Update(p.window.Close)
		},
		measure: func() {
			if p.window != nil {
				p.window.Update(func() { m.update(p) })
			}
		},
	}
	r := place(work, source, p.view.limit, nil)
	p.window = mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 译文", Content: ui.View(p.view.render),
		X: r.X, Y: r.Y, Width: r.Width, Height: r.Height,
		Hidden: true, Frameless: true, Transparent: true, Panel: true,
		SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
		DisableMaximize: true, DisableFullScreen: true, DisableShadow: true,
	})
	p.window.SetVisibleOnAllWorkspaces(false)
	m.windows = append(m.windows, p)
	p.window.OnReadyToShow(func() {
		p.ready = true
		m.update(p)
	})
	p.window.OnClosed(func() {
		m.windows = slices.DeleteFunc(m.windows, func(other *Result) bool { return p == other })
		p.cancelPending()
	})
	return p
}

func (p *Result) Progress(text string) {
	if p.onCancel == nil || p.window.IsDestroyed() {
		return
	}
	p.view.setText(text, true)
	p.window.Invalidate()
}

func (p *Result) Complete(text, message string) {
	if p.window.IsDestroyed() {
		return
	}
	p.onCancel = nil
	p.view.setText(resultText(text, message), false)
	if message != "" {
		p.view.scroll.Y = 0
	}
	// Retain a size for restoring a window during screenshot capture. A
	// concealed native window may not lay out the new text until it is shown.
	p.window.Invalidate()
}

func (p *Result) Close() {
	p.window.Close()
}

func (p *Result) cancelPending() {
	if cancel := p.onCancel; cancel != nil {
		p.onCancel = nil
		cancel()
	}
}

func resultText(text, message string) string {
	switch {
	case message == "":
		return text
	case text == "":
		return message
	default:
		return "译文未完成：" + message + "\n\n" + text
	}
}

func workArea(r mygo.Rectangle) mygo.Rectangle {
	point := mygo.Point{X: r.X + r.Width/2, Y: r.Y + r.Height/2}
	return mygo.Screen.DisplayNearestPoint(point).WorkArea
}

func (m *Manager) update(p *Result) {
	if !p.ready || p.window.IsDestroyed() || m.suspended || p.view.measured == (size{}) {
		return
	}
	if p.workspace != platform.WorkspaceGeneration() {
		p.window.Close()
		return
	}
	if measured := p.view.measured; measured != p.applied {
		var occupied []mygo.Rectangle
		if !p.moved {
			for _, other := range m.windows {
				if other != p && other.presented {
					if bounds := other.window.Bounds(); overlap(p.work, bounds) > 0 {
						occupied = append(occupied, bounds)
					}
				}
			}
		}
		var r mygo.Rectangle
		if p.presented {
			r = resize(p.work, p.source, p.window.Bounds(), measured, p.moved, occupied)
		} else {
			r = place(p.work, p.source, measured, occupied)
		}
		p.applied = measured
		p.window.SetBounds(r)
	}
	if !p.window.IsVisible() {
		p.presented = true
		p.window.ShowInactive()
	}
	p.animateLoading(p.window.IsVisible())
}

// Suspend keeps results out of both the desktop snapshot and the selection UI.
func (m *Manager) Suspend() func() {
	m.suspended = true
	for _, p := range m.windows {
		p.animateLoading(false)
		p.window.Hide()
	}
	return func() {
		m.suspended = false
		for _, p := range slices.Clone(m.windows) {
			m.update(p)
		}
	}
}

func (m *Manager) Close() {
	for _, p := range slices.Clone(m.windows) {
		p.window.Close()
	}
}

// CloseStale retires old workspace results without touching new ones when a
// workspace notification arrives after an entry in the new workspace.
func (m *Manager) CloseStale(workspace uint64) {
	for _, p := range slices.Clone(m.windows) {
		if p.workspace != workspace {
			p.window.Close()
		}
	}
}

func (m *Manager) RefreshDisplays() {
	for _, p := range m.windows {
		r := p.window.Bounds()
		p.setWorkArea(workArea(r))
		p.window.SetBounds(fit(p.work, r))
	}
}

func (p *Result) setWorkArea(work mygo.Rectangle) {
	p.work = work
	if limit := limits(work); limit != p.view.limit {
		p.view.limit = limit
		p.window.Invalidate()
	}
}

func (p *Result) animateLoading(visible bool) {
	animate := p.view.pending && p.view.text == "" && visible
	if p.view.animate != animate {
		p.view.animate = animate
		p.window.Invalidate()
	}
}
