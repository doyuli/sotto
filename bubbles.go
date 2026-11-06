package main

import (
	"fmt"
	"sync"

	"github.com/egoist/mygo"

	"sotto/internal/config"
)

// bubbleManager owns the bubble windows. One unpinned bubble is kept and
// reused for every query — hidden, not destroyed, so it reappears at
// once — and pinning it moves it aside: the next query then opens a new
// window, and any number of pinned bubbles can coexist.
//
// Threading: Show and ShowAt run on goroutines — hotkey handlers and
// tray clicks dispatch to them, the main thread never calls in — and
// showMu serializes them with each other. The mutex m.mu guards the maps
// and the bubbles' mutable fields, and is never held across a call that
// waits for the main thread (window calls wait there): the main thread's
// callbacks — close, hide, ready — take m.mu for plain memory only, so
// whoever holds it releases within microseconds and nothing deadlocks.
type bubbleManager struct {
	showMu  sync.Mutex
	mu      sync.Mutex
	nextID  int64
	current *bubble   // the reusable unpinned bubble
	pinned  []*bubble // pinned bubbles, oldest first
	byID    map[int64]*bubble
	// onReuse cancels the previous run of a bubble that is reused.
	onReuse func(prevReqID string)
	// isQuitting reports a quit under way: the close handlers then let
	// windows close instead of hiding, or the quit sequence would abort.
	isQuitting func() bool
}

type bubble struct {
	id     int64
	win    *mygo.Window
	pinned bool
	reqID  string // the request its page last showed
	// trigger and text of the last query, for a page that mounts late.
	trigger, text string
	// ready reports the page has loaded once; pendingShow shows a fresh
	// window when it has, to avoid a flash of blank window.
	ready       bool
	pendingShow func()
	offBlur     func() // unregisters focus hiding; Pin calls it
}

// Anchor is a DIP screen point to open a bubble beside — a selection's
// corner, when the grab could see one.
type Anchor struct{ X, Y int }

// Shown describes the bubble a Show brought up: the window that carries
// it and the query it holds, for whoever runs that query.
type Shown struct {
	Win   *mygo.Window
	ReqID string
	Text  string
}

func newBubbleManager(onReuse func(prevReqID string), isQuitting func() bool) *bubbleManager {
	return &bubbleManager{byID: map[int64]*bubble{}, onReuse: onReuse, isQuitting: isQuitting}
}

// Show brings up a bubble beside the pointer.
func (m *bubbleManager) Show(cfg config.Settings, trigger, text string) Shown {
	return m.ShowAt(cfg, trigger, text, nil)
}

// ShowAt brings up a bubble for a query: the unpinned one if it is
// around, a fresh window otherwise. Selection and OCR bubbles appear
// without taking the focus from the app the user was in; an input bubble
// takes it, as a launcher's panel does.
func (m *bubbleManager) ShowAt(cfg config.Settings, trigger, text string, anchor *Anchor) Shown {
	if trigger == "input" {
		text = "" // an input bubble starts empty
	}
	m.showMu.Lock()
	defer m.showMu.Unlock()

	m.mu.Lock()
	b := m.current
	// A reused bubble stops its previous run.
	if b != nil && m.onReuse != nil && b.reqID != "" {
		m.onReuse(b.reqID)
	}
	m.mu.Unlock()
	if b == nil {
		// Creating a window waits for the main thread, which stays free
		// to take m.mu meanwhile (the callbacks): create outside it.
		b = m.create(cfg)
		m.mu.Lock()
		m.current = b
		m.mu.Unlock()
	}
	m.mu.Lock()
	b.reqID = newRequestID()
	b.trigger, b.text = trigger, text
	state := m.state(b, cfg)
	m.mu.Unlock()

	m.position(b, trigger, anchor)
	BubbleReset.Emit(b.win, state)
	m.showWin(b, trigger)
	return Shown{Win: b.win, ReqID: b.reqID, Text: text}
}

// Pin makes the bubble stay when it loses focus; the manager forgets it
// as the reusable one, so the next query opens a new window.
func (m *bubbleManager) Pin(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.byID[id]
	if !ok || b.pinned {
		return
	}
	b.pinned = true
	if b.offBlur != nil {
		b.offBlur()
		b.offBlur = nil
	}
	if m.current == b {
		m.current = nil
	}
	m.pinned = append(m.pinned, b)
}

// Unpin returns a pinned bubble to the transient life: it hides on blur
// again and becomes the bubble the next query reuses. The bubble it
// displaces is no longer the reusable one, and such leftovers destroy
// themselves once hidden (see the OnHide handler in create).
func (m *bubbleManager) Unpin(cfg config.Settings, id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.byID[id]
	if !ok || !b.pinned {
		return
	}
	b.pinned = false
	if i := indexOfBubble(m.pinned, b); i >= 0 {
		m.pinned = append(m.pinned[:i], m.pinned[i+1:]...)
	}
	m.current = b
	if cfg.Bubble.AutoHide && b.offBlur == nil {
		b.offBlur = b.win.OnBlur(b.win.Hide)
	}
}

// Close hides an unpinned bubble and closes a pinned one for good.
func (m *bubbleManager) Close(id int64) {
	m.mu.Lock()
	b, ok := m.byID[id]
	m.mu.Unlock()
	if ok {
		b.win.Close()
	}
}

// setAutoHide turns the unpinned bubble's hide-on-blur on or off; it
// runs from applySettings, on the main thread.
func (m *bubbleManager) setAutoHide(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.current
	if b == nil {
		return
	}
	switch {
	case on && b.offBlur == nil:
		b.offBlur = b.win.OnBlur(b.win.Hide)
	case !on && b.offBlur != nil:
		b.offBlur()
		b.offBlur = nil
	}
}

// State returns the bubble's current state for its page.
func (m *bubbleManager) State(cfg config.Settings, id int64) (BubbleState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.byID[id]
	if !ok {
		return BubbleState{}, false
	}
	return m.state(b, cfg), true
}

// windows lists every open bubble window, for broadcasting settings.
func (m *bubbleManager) windows() []*mygo.Window {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*mygo.Window
	if m.current != nil {
		out = append(out, m.current.win)
	}
	for _, b := range m.pinned {
		out = append(out, b.win)
	}
	return out
}

func (m *bubbleManager) state(b *bubble, cfg config.Settings) BubbleState {
	return BubbleState{
		ID:         b.id,
		ReqID:      b.reqID,
		Trigger:    b.trigger,
		Text:       b.text,
		SourceLang: "auto",
		TargetLang: cfg.TargetLanguage,
		Pinned:     b.pinned,
		Settings:   cfg,
	}
}

// showWin displays the bubble now, or as soon as its page is ready.
// Every bubble takes the focus when it shows: the hide-on-blur that
// makes a bubble go away when the user clicks elsewhere only fires on a
// window that has the focus, and a focusless overlay (ShowInactive)
// would linger forever. Esc, or a click outside, hides it and the
// previous app gets the focus back.
func (m *bubbleManager) showWin(b *bubble, _ string) {
	show := b.win.Show
	m.mu.Lock()
	if b.ready {
		m.mu.Unlock()
		show()
		return
	}
	b.pendingShow = show
	m.mu.Unlock()
}

// create makes a hidden bubble window. It runs outside m.mu — creating
// a window waits for the main thread — but under showMu, so no two
// bubbles are ever created at once.
func (m *bubbleManager) create(cfg config.Settings) *bubble {
	id := m.nextID
	m.nextID++
	b := &bubble{id: id}
	b.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "Sotto",
		URL:             fmt.Sprintf("/#/bubble?b=%d", id),
		Width:           480,
		Height:          240,
		MinWidth:        480,
		MaxWidth:        480,
		MinHeight:       160,
		MaxHeight:       760,
		DisableResize:   true, // the bubble has a fixed width
		FitContent:      true, // the bubble grows with its cards
		Hidden:          true, // shown once placed and ready
		Frameless:       true,
		Panel:           true, // floats over every app, out of the taskbar
		SkipTaskbar:     true,
		Transparent:     true, // the page draws its rounded card
		CornerRadius:    14,
		BackgroundColor: "light-dark(#ffffff, #222325)",
		Page:            mygo.PageOptions{DevTools: mygo.DevToolsAuto},
	})
	b.win.OnReadyToShow(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		b.ready = true
		if b.pendingShow != nil {
			show := b.pendingShow
			b.pendingShow = nil
			show()
		}
	})
	b.win.OnClose(func(e *mygo.CloseEvent) {
		// The app quitting closes its windows for real: mygo's quit
		// sequence aborts when a window refuses to close, and a bubble
		// hiding instead would keep the app alive forever.
		quitting := m.isQuitting != nil && m.isQuitting()
		m.mu.Lock()
		pinned := b.pinned
		m.mu.Unlock()
		if quitting || pinned {
			return // close it for real
		}
		e.PreventDefault()
		b.win.Hide()
	})
	b.win.OnClosed(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.byID, b.id)
		if m.current == b {
			m.current = nil
		}
		if i := indexOfBubble(m.pinned, b); i >= 0 {
			m.pinned = append(m.pinned[:i], m.pinned[i+1:]...)
		}
	})
	// An unpinned bubble that is no longer the reusable one — displaced
	// by Unpin — will never be shown again: recycle it once hidden. The
	// reusable one, and pinned ones, survive their hides.
	b.win.OnHide(func() {
		m.mu.Lock()
		stale := !b.pinned && m.current != b
		m.mu.Unlock()
		if stale {
			b.win.Destroy()
		}
	})
	if cfg.Bubble.AutoHide {
		b.offBlur = b.win.OnBlur(b.win.Hide)
	}
	m.mu.Lock()
	m.byID[id] = b
	m.mu.Unlock()
	return b
}

// position puts the bubble near where the user is: beside an anchor
// when the grab could see the selection, beside the pointer for an OCR
// shot, and an input bubble a quarter of the way down its display.
func (m *bubbleManager) position(b *bubble, trigger string, anchor *Anchor) {
	var x, y int
	var work mygo.Rectangle
	if anchor != nil {
		work = mygo.Screen.DisplayNearestPoint(mygo.Point{X: anchor.X, Y: anchor.Y}).WorkArea
		x, y = anchor.X+16, anchor.Y+16
	} else {
		point := mygo.Screen.CursorScreenPoint()
		work = mygo.Screen.DisplayNearestPoint(point).WorkArea
		if trigger == "input" {
			width, _ := b.win.Size()
			x = work.X + (work.Width-width)/2
			y = work.Y + work.Height/4
		} else {
			x, y = point.X+16, point.Y+16
		}
	}
	width, height := b.win.Size()
	// Keep the bubble inside its display's work area.
	x = min(max(x, work.X), work.X+max(work.Width-width, 0))
	y = min(max(y, work.Y), work.Y+max(work.Height-height, 0))
	b.win.SetPosition(x, y)
}

func indexOfBubble(bs []*bubble, b *bubble) int {
	for i, x := range bs {
		if x == b {
			return i
		}
	}
	return -1
}
