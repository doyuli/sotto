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
	windows       []*Result
	suspended     bool
	status        *mygo.Window
	pendingStatus string
}

// Result is a floating translation that can be completed in place.
// Its methods run on the main thread.
type Result struct {
	window    *mygo.Window
	source    mygo.Rectangle
	work      mygo.Rectangle
	workspace uint64
	view      resultView
	input     mygo.Rectangle
	ready     bool
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
	limit := limits(work)
	r := place(work, source, limit, nil)
	p := &Result{source: source, work: work, workspace: workspace, onCancel: onCancel}
	p.view = resultView{
		text: text, pending: onCancel != nil, measuring: true, limit: limit,
		cursor: mygo.Screen.CursorScreenPoint,
		position: func() mygo.Point {
			r := p.visibleBounds(p.window.Bounds())
			return mygo.Point{X: r.X, Y: r.Y}
		},
		move: func(point mygo.Point) {
			r := p.visibleBounds(p.window.Bounds())
			r.X, r.Y = point.X, point.Y
			p.setWorkArea(workArea(r))
			p.placePanel(r, false, false)
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
				p.window.Invalidate()
			}
		},
		boundsChanged: func() {
			if p.window != nil {
				p.window.Update(func() { m.update(p) })
			}
		},
	}
	p.window = mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 译文", Content: ui.View(p.view.render),
		X: r.X, Y: r.Y, Width: r.Width, Height: r.Height,
		Hidden: true, Frameless: true, Transparent: true, Panel: true,
		SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
		DisableMaximize: true, DisableFullScreen: true, DisableShadow: true,
	})
	platform.ConfigureOverlayWindow(p.window)
	if p.window.Bounds() != r {
		p.window.SetBounds(r)
	}
	p.window.SetVisibleOnAllWorkspaces(false)
	p.window.SetInputRegion([]mygo.Rectangle{})
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

func (p *Result) visibleBounds(native mygo.Rectangle) mygo.Rectangle {
	r := p.view.bounds
	r.X += native.X
	r.Y += native.Y
	return r
}

func (m *Manager) update(p *Result) {
	if !p.ready || p.window.IsDestroyed() || m.suspended {
		return
	}
	if p.workspace != platform.WorkspaceGeneration() {
		p.window.Close()
		return
	}
	if p.view.measuring {
		if p.view.bounds.Width <= 0 || p.view.bounds.Height <= 0 {
			return
		}
		var occupied []mygo.Rectangle
		for _, other := range m.windows {
			if other != p && !other.view.measuring {
				bounds := other.window.Bounds()
				if !other.view.pending && other.view.bounds.Width > 0 {
					bounds = other.visibleBounds(bounds)
				}
				if overlap(p.work, bounds) > 0 {
					occupied = append(occupied, bounds)
				}
			}
		}
		panelSize := p.view.measured
		if panelSize == (size{}) {
			panelSize = size{p.view.bounds.Width, p.view.bounds.Height}
		}
		panel := initialPanel(p.work, p.source, panelSize, p.view.limit, occupied)
		p.placePanel(panel, panel.X+panel.Width <= p.source.X, panel.Y+panel.Height <= p.source.Y)
		// The first frame only measures the card. Show now so Windows can
		// draw the placed card, then use that frame's bounds for its input.
		p.view.measuring = false
		p.view.bounds = mygo.Rectangle{}
		p.window.Invalidate()
	}
	if bounds := p.view.bounds; bounds != p.input {
		// The transparent space around the animated panel lets clicks and wheel events through.
		p.input = bounds
		p.window.SetInputRegion([]mygo.Rectangle{bounds})
	}
	if !p.window.IsVisible() {
		p.window.ShowInactive()
	}
	p.animateLoading(p.window.IsVisible())
}

// Suspend keeps results out of both the desktop snapshot and the selection UI.
func (m *Manager) Suspend() func() {
	m.suspended = true
	if m.status != nil {
		m.status.Close()
	}
	for _, p := range m.windows {
		p.animateLoading(false)
		p.window.Hide()
	}
	return func() {
		m.suspended = false
		for _, p := range slices.Clone(m.windows) {
			m.update(p)
		}
		if message := m.pendingStatus; message != "" {
			m.pendingStatus = ""
			m.ShowStatus(message)
		}
	}
}

func (m *Manager) Close() {
	m.pendingStatus = ""
	if m.status != nil {
		m.status.Close()
	}
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
		if p.view.measuring {
			p.setWorkArea(workArea(p.source))
			continue
		}
		r := p.window.Bounds()
		visible := p.visibleBounds(r)
		p.setWorkArea(workArea(visible))
		p.placePanel(visible, p.view.alignRight, p.view.alignBottom)
	}
}

func (p *Result) placePanel(visible mygo.Rectangle, right, bottom bool) {
	frame, inset, right, bottom := reservePanel(p.work, visible, p.view.limit, right, bottom)
	changed := p.view.inset != inset || p.view.alignRight != right || p.view.alignBottom != bottom
	p.view.inset, p.view.alignRight, p.view.alignBottom = inset, right, bottom
	if frame != p.window.Bounds() {
		p.window.SetBounds(frame)
	}
	if changed {
		p.window.Invalidate()
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
