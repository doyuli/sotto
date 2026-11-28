package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/platform"
	"sotto/internal/selection"
	"sotto/internal/ui/shared"
)

const (
	selectionButtonSize = 30
	selectionButtonWait = 1500 * time.Millisecond
)

// The selection button and its pending probe belong to the main thread.
type selectionButton struct {
	win           *mygo.Window
	watch         *selectionButtonWatch
	cancel        context.CancelFunc
	grab          func(context.Context) (selection.Selection, error)
	gesture       selectionButtonGesture
	ignoreThrough uint64
}

type selectionButtonWatch struct {
	stop       func()
	version    atomic.Uint64
	lastCancel atomic.Uint64
}

type selectionPointerInput struct {
	selection.SelectionPointer
	version uint64
}

func (a *application) updateSelectionButton() {
	b := &a.selectionButton
	if !a.settings().SelectionButton {
		b.shutdown()
		return
	}
	if b.watch != nil || a.isQuitting() {
		return
	}
	watch := &selectionButtonWatch{}
	input := make(chan selectionPointerInput, 32)
	done := make(chan struct{})
	stop, err := selection.WatchSelectionPointer(func(event selection.SelectionPointer) {
		watch.enqueue(input, event)
	})
	if err != nil {
		if !errors.Is(err, selection.ErrUnsupported) {
			log.Printf("watch selection button input: %v", err)
		}
		return
	}
	watch.stop = func() { close(done); stop() }
	b.watch = watch
	b.ignoreThrough = 0
	go func() {
		for {
			select {
			case <-done:
				return
			case event := <-input:
				mygo.RunOnMain(func() { a.handleSelectionPointer(watch, event) })
			}
		}
	}()
}

func (watch *selectionButtonWatch) enqueue(input chan<- selectionPointerInput, event selection.SelectionPointer) {
	next := selectionPointerInput{SelectionPointer: event, version: watch.version.Add(1)}
	if event.Kind == selection.SelectionPointerCancel {
		watch.invalidateThrough(next.version)
	}
	select {
	case input <- next:
		return
	default:
	}

	// A dropped edge makes pending input ambiguous. Publish the same
	// invalidation used for keyboard, scroll, and foreground changes.
	watch.invalidateThrough(next.version)
	select {
	case input <- next:
	default: // Queued input will apply the invalidation when the UI catches up.
	}
}

func (watch *selectionButtonWatch) invalidateThrough(version uint64) {
	for previous := watch.lastCancel.Load(); previous < version; previous = watch.lastCancel.Load() {
		if watch.lastCancel.CompareAndSwap(previous, version) {
			return
		}
	}
}

func (b *selectionButton) dismiss() {
	if b.watch != nil {
		b.ignoreThrough = b.watch.version.Load()
	}
	b.resetGesture()
}

func (b *selectionButton) resetGesture() {
	b.hide()
	b.gesture = selectionButtonGesture{}
}

func (b *selectionButton) hide() {
	b.grab = nil
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.win != nil {
		b.win.Hide()
	}
}

func (b *selectionButton) shutdown() {
	b.dismiss()
	if b.watch != nil {
		b.watch.stop()
		b.watch = nil
	}
	if b.win != nil {
		b.win.Destroy()
		b.win = nil
	}
}

func (a *application) handleSelectionPointer(watch *selectionButtonWatch, event selectionPointerInput) {
	b := &a.selectionButton
	if b.watch != watch || a.isQuitting() || !b.acceptPointerInput(event.version) {
		return
	}
	event.Point = selection.SelectionPointerScreenPoint(event.Point)
	if b.win != nil && b.win.IsVisible() && selectionButtonContains(b.win.Bounds(), event.Point) {
		return
	}
	if event.Kind == selection.SelectionPointerDown {
		b.hide()
	}
	if !b.gesture.update(event.SelectionPointer) || event.version != watch.version.Load() {
		return
	}

	// Give the source app time to commit the selection after mouse-up.
	// Cancellation covers a new gesture, a hotkey entry, and shutdown.
	ctx, cancel := context.WithDeadline(context.Background(), event.At.Add(selectionButtonWait))
	b.cancel = cancel
	probe, checkSource := selection.PreparePointerSelection(event.Point)
	go func() {
		defer cancel()
		sel, ok := probeSelectionButton(ctx, probe)
		if !ok {
			return
		}
		mygo.RunOnMain(func() {
			if ctx.Err() == nil && b.watch == watch && event.version == watch.version.Load() && !a.isQuitting() {
				a.showSelectionButton(event.Point, watch.confirmedSelection(event.version, sel, checkSource))
			}
		})
	}()
}

func (b *selectionButton) acceptPointerInput(version uint64) bool {
	if canceled := b.watch.lastCancel.Load(); canceled > b.ignoreThrough {
		b.ignoreThrough = canceled
		b.resetGesture()
	}
	return version > b.ignoreThrough
}

func probeSelectionButton(ctx context.Context, probe func(context.Context) (selection.Selection, error)) (selection.Selection, bool) {
	// One later retry covers providers that initially report an empty selection.
	for _, delay := range [...]time.Duration{75 * time.Millisecond, 75 * time.Millisecond, 350 * time.Millisecond} {
		select {
		case <-ctx.Done():
			return selection.Selection{}, false
		case <-time.After(delay):
		}
		if ctx.Err() != nil {
			return selection.Selection{}, false
		}
		sel, err := probe(ctx)
		if err != nil && !errors.Is(err, selection.ErrNoSelection) {
			return selection.Selection{}, false
		}
		if err == nil && strings.TrimSpace(sel.Text) != "" && ctx.Err() == nil {
			return sel, true
		}
	}
	return selection.Selection{}, false
}

func (watch *selectionButtonWatch) confirmedSelection(version uint64, sel selection.Selection, checkSource func(context.Context) error) func(context.Context) (selection.Selection, error) {
	return func(ctx context.Context) (selection.Selection, error) {
		if err := checkSource(ctx); err != nil {
			return selection.Selection{}, err
		}
		// An input event can invalidate the selection before its dismissal
		// is processed on the UI thread.
		if watch.lastCancel.Load() > version {
			return selection.Selection{}, selection.ErrSourceChanged
		}
		return sel, nil
	}
}

func (a *application) showSelectionButton(point mygo.Point, grab func(context.Context) (selection.Selection, error)) {
	b := &a.selectionButton
	if b.win == nil {
		var w *mygo.Window
		translate := func() {
			if b.win == w && w.IsVisible() && b.grab != nil && a.settings().SelectionButton {
				a.startEntryWithSelection("selection", b.grab)
			}
		}
		w = mygo.NewWindow(mygo.WindowOptions{
			Title: "划词翻译", Hidden: true, Panel: true,
			Width: selectionButtonSize, Height: selectionButtonSize, UseContentSize: true,
			Frameless: true, DisableResize: true, DisableMove: true, Transparent: true,
			DisableShadow: true,
			Content:       ui.View(func(c *ui.Context) { renderSelectionButton(c, translate) }),
		})
		if err := platform.ConfigureSelectionButtonWindow(w, translate); err != nil {
			log.Printf("configure selection button window: %v", err)
			w.Destroy()
			return
		}
		b.win = w
		w.OnClosed(func() {
			if b.win == w {
				b.win = nil
				b.grab = nil
			}
		})
	}
	work := mygo.Screen.DisplayNearestPoint(point).WorkArea
	position := selectionButtonPosition(point, work)
	b.grab = grab
	b.win.SetPosition(position.X, position.Y)
	b.win.ShowInactive()
}

func renderSelectionButton(c *ui.Context, translate func()) {
	c.Root().Background(ui.Transparent)
	accent, hover, letter := ui.Hex("#2f7fe8"), ui.Hex("#1a5fae"), ui.Hex("#ffffff")
	if c.Theme().Dark {
		accent, hover, letter = ui.Hex("#7db4ff"), ui.Hex("#a5cbff"), ui.Hex("#222325")
	}
	button := ui.ButtonBase(c).Fill().Center().Label("翻译选中文字").OnClick(translate)
	if button.Hovered() {
		accent = hover
	}
	button.Children(func() { shared.BrandMark(c, accent, letter) })
}

type selectionButtonGesture struct {
	down    selection.SelectionPointer
	lastTap selection.SelectionPointer
}

func (g *selectionButtonGesture) update(event selection.SelectionPointer) bool {
	if event.Kind == selection.SelectionPointerDown {
		g.down = event
		return false
	}
	if event.Kind != selection.SelectionPointerUp || g.down.At.IsZero() {
		return false
	}
	from := g.down.Point
	g.down = selection.SelectionPointer{}
	if !selectionButtonNear(from, event.Point, 5) {
		g.lastTap = selection.SelectionPointer{}
		return true
	}
	previous := g.lastTap
	g.lastTap = event
	gap := event.At.Sub(previous.At)
	return gap > 0 && gap <= 500*time.Millisecond && selectionButtonNear(previous.Point, event.Point, 5)
}

func selectionButtonNear(a, b mygo.Point, distance int) bool {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx >= -distance && dx <= distance && dy >= -distance && dy <= distance
}

func selectionButtonContains(bounds mygo.Rectangle, point mygo.Point) bool {
	return point.X >= bounds.X && point.X < bounds.X+bounds.Width &&
		point.Y >= bounds.Y && point.Y < bounds.Y+bounds.Height
}

func selectionButtonPosition(point mygo.Point, work mygo.Rectangle) mygo.Point {
	const gap = 8
	x, y := point.X+gap, point.Y+gap
	if x+selectionButtonSize > work.X+work.Width {
		x = point.X - gap - selectionButtonSize
	}
	if y+selectionButtonSize > work.Y+work.Height {
		y = point.Y - gap - selectionButtonSize
	}
	if work.Width > 0 && work.Height > 0 {
		x = max(work.X, min(x, work.X+work.Width-selectionButtonSize))
		y = max(work.Y, min(y, work.Y+work.Height-selectionButtonSize))
	}
	return mygo.Point{X: x, Y: y}
}
