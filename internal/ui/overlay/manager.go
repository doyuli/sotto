// Package overlay presents independent, text-only translation results.
package overlay

import (
	"errors"
	"log"
	"runtime"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/platform"
	"sotto/internal/selection"
)

// Manager and its windows belong to the main thread.
type Manager struct {
	windows       []*Result
	autoHide      bool
	latest        int64
	suspended     bool
	status        *mygo.Window
	pendingStatus string
}

// Source identifies the request and where to place its result.
type Source struct {
	EntryID   int64
	Bounds    mygo.Rectangle
	Workspace uint64
}

// Result is a floating translation that can be completed in place.
// Its methods run on the main thread.
type Result struct {
	window    *mygo.Window
	source    Source
	work      mygo.Rectangle
	view      resultView
	input     mygo.Rectangle
	ready     bool
	onCancel  func()
	stopLeave func()
}

func (m *Manager) Show(source Source, text, message string) {
	m.show(source, resultText(text, message), nil)
}

// ShowPending creates a cancellable window with either a loading state or
// the first cumulative snapshot from the provider.
func (m *Manager) ShowPending(source Source, text string, onCancel func()) *Result {
	return m.show(source, text, onCancel)
}

func (m *Manager) show(source Source, text string, onCancel func()) *Result {
	if m.superseded(source.EntryID) {
		if onCancel != nil {
			onCancel()
		}
		return nil
	}
	work := workArea(source.Bounds)
	limit := limits(work)
	r := place(work, source.Bounds, limit, nil)
	p := &Result{source: source, work: work, onCancel: onCancel}
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
		p.unwatchLeave()
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
	if p.source.Workspace != platform.WorkspaceGeneration() {
		p.Close()
		return
	}
	if m.superseded(p.source.EntryID) {
		p.Close()
		return
	}
	if p.view.measuring {
		if p.view.bounds.Width <= 0 || p.view.bounds.Height <= 0 {
			return
		}
		var occupied []mygo.Rectangle
		for _, other := range m.windows {
			if other != p && !m.autoHide && !other.view.measuring {
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
		panel := initialPanel(p.work, p.source.Bounds, panelSize, p.view.limit, occupied)
		p.placePanel(panel, panel.X+panel.Width <= p.source.Bounds.X, panel.Y+panel.Height <= p.source.Bounds.Y)
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
		if m.autoHide {
			m.retireOlder(p.source.EntryID)
		}
		p.window.ShowInactive()
		if m.autoHide {
			p.watchLeave()
		}
	}
	p.animateLoading(p.window.IsVisible())
}

// SetAutoHide applies the preference to open results as well as future ones.
func (m *Manager) SetAutoHide(on bool) {
	if m.autoHide == on {
		return
	}
	m.autoHide = on
	if !on {
		m.latest = 0
		for _, p := range m.windows {
			p.unwatchLeave()
		}
		return
	}
	var latest *Result
	for _, p := range m.windows {
		if !p.view.measuring && (latest == nil || p.source.EntryID > latest.source.EntryID) {
			latest = p
		}
	}
	if latest != nil {
		m.retireOlder(latest.source.EntryID)
		if latest.window.IsVisible() {
			latest.watchLeave()
		}
	}
}

func (m *Manager) superseded(id int64) bool {
	return m.autoHide && id < m.latest
}

// The newest result takes over only when it is ready to show. Keep its ID
// after dismissal so a delayed callback from an older request cannot reopen it.
func (m *Manager) retireOlder(id int64) {
	m.latest = id
	for _, p := range slices.Clone(m.windows) {
		if m.superseded(p.source.EntryID) {
			p.Close()
		}
	}
}

func (p *Result) watchLeave() {
	w := p.window
	active := true
	dismiss := func(nativeLeave bool) {
		w.Update(func() {
			if active && w.IsVisible() &&
				(nativeLeave && runtime.GOOS == "darwin" || !selection.WindowHasFocus(w)) {
				p.Close()
			}
		})
	}
	offBlur := w.OnBlur(func() { dismiss(false) })
	stop, err := selection.WatchWindowLeave(w.NativeHandle(), func(selection.PrimaryPress) {
		// A Mac panel may remain key after another app has activated.
		dismiss(true)
	})
	if err != nil && !errors.Is(err, selection.ErrUnsupported) {
		log.Printf("watch minimal translation window: %v", err)
	}
	p.stopLeave = func() {
		active = false
		offBlur()
		if stop != nil {
			stop()
		}
	}
}

func (p *Result) unwatchLeave() {
	if p.stopLeave != nil {
		p.stopLeave()
		p.stopLeave = nil
	}
}

// Suspend keeps results out of both the desktop snapshot and the selection UI.
func (m *Manager) Suspend() func() {
	m.suspended = true
	if m.status != nil {
		m.status.Close()
	}
	for _, p := range m.windows {
		p.unwatchLeave()
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
		if p.source.Workspace != workspace {
			p.window.Close()
		}
	}
}

func (m *Manager) RefreshDisplays() {
	for _, p := range m.windows {
		if p.view.measuring {
			p.setWorkArea(workArea(p.source.Bounds))
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
