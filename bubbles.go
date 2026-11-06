package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/selection"
)

// bubbleManager keeps an input draft and a separate reusable capture bubble.
// Pinning detaches either window from reuse; pinned windows remain independent.
//
// ShowTask enters from a worker and holds showMu while marshaling presentation
// to the UI thread. Never call ShowTask itself from the UI thread: another
// worker may hold showMu while waiting for that thread. Presentation and Close
// commit on the UI thread; m.mu guards state shared with translation workers.
// Release m.mu before native window calls. See docs/translation-state.md for
// the identities and ordering rules used by backend state and frontend events.
type bubbleManager struct {
	showMu  sync.Mutex
	mu      sync.Mutex
	nextID  int64
	input   *bubble
	current *bubble   // the reusable unpinned bubble
	pinned  []*bubble // pinned bubbles, oldest first
	byID    map[int64]*bubble
	// isQuitting reports a quit under way: the close handlers then let
	// windows close instead of hiding, or the quit sequence would abort.
	isQuitting func() bool
}

type bubble struct {
	id                     int64
	win                    *mygo.Window
	pinned                 bool
	reqID                  string // the request its page last showed
	generation             int64
	showID                 int64
	closeClaimed           bool
	sessionID              string
	revision               int64
	fullRevision           int64 // last full run or cancellation; retries only advance their own service
	fullReqID              string
	serviceRevisions       map[string]int64
	sourceLang, targetLang string
	phase, message         string
	meta                   *RunMetaMsg
	results                map[string]TranslationUpdateMsg
	runCancels             map[string]context.CancelFunc
	serviceRequests        map[string]string
	cancelCapture          context.CancelFunc
	canShow                func() bool
	// trigger and text of the last query, for a page that mounts late.
	trigger, text string
	// ready reports the page has loaded once; pendingShow shows a fresh
	// window when it has, to avoid a flash of blank window.
	ready       bool
	pendingShow func()
	offBlur     func() // unregisters focus hiding; Pin calls it
	// offOutsideClick belongs to the main thread, like the window it watches.
	offOutsideClick func()
}

// Anchor is a DIP screen point to open a bubble beside — a selection's
// corner, when the grab could see one.
type Anchor struct{ X, Y int }

// Shown identifies an accepted presentation; a new page may still be loading.
// Win is nil when entry ownership expired or UI dispatch stopped during quit.
type Shown struct {
	Win   *mygo.Window
	ReqID string
	Text  string
}

func newBubbleManager(isQuitting func() bool) *bubbleManager {
	return &bubbleManager{byID: map[int64]*bubble{}, isQuitting: isQuitting}
}

// ShowTask separates the persistent input draft from transient captures.
// valid is checked again on the UI thread, including a delayed first show.
// Call from a worker only, and check Shown.Win before starting follow-up work.
func (m *bubbleManager) ShowTask(cfg config.Settings, trigger, text string, anchor *Anchor, phase, message string, valid func() bool, cancel context.CancelFunc) Shown {
	m.showMu.Lock()
	defer m.showMu.Unlock()
	if valid != nil && !valid() {
		return Shown{}
	}
	m.mu.Lock()
	b, other := m.current, m.input
	if trigger == "input" {
		b, other = m.input, m.current
	}
	m.mu.Unlock()
	created := b == nil
	if created {
		b = m.create(cfg)
	}
	if valid != nil && !valid() {
		if created {
			b.win.Destroy()
		}
		return Shown{}
	}
	var shown Shown
	// Presentation and close commit on the same UI thread. A delayed close
	// cannot pass validation and then race a new presentation before Hide.
	mygo.RunOnMain(func() {
		if valid != nil && !valid() {
			if created {
				b.win.Destroy()
			}
			return
		}
		if other != nil {
			m.hideTransientBubble(other)
		}
		m.mu.Lock()
		pinned := b.pinned
		m.mu.Unlock()
		if pinned {
			b = m.create(cfg)
		}
		m.mu.Lock()
		reuseInput := trigger == "input" && b.sessionID != ""
		if trigger == "input" {
			m.input = b
		} else {
			m.current = b
		}
		if !reuseInput {
			b.stopWork()
			b.generation++
			b.sessionID = newRequestID()
			b.reqID = b.sessionID
			b.revision = -1
			b.fullRevision, b.fullReqID = -1, ""
			b.serviceRevisions = nil
			b.trigger, b.text = trigger, text
			b.sourceLang, b.targetLang = "auto", cfg.TargetLanguage
			b.phase, b.message = phase, message
			b.meta, b.results = nil, nil
			b.serviceRequests = nil
			b.cancelCapture = cancel
		}
		b.present()
		b.canShow = valid
		state := m.state(b, cfg)
		m.mu.Unlock()
		if !reuseInput {
			m.position(b, trigger, anchor)
			BubbleReset.Emit(b.win, state)
		}
		m.showWin(b)
		BubblePresented.Emit(b.win, BubblePresentedMsg{ClipboardRecoveryID: state.ClipboardRecoveryID, SessionID: state.SessionID, ShowID: state.ShowID, Revision: state.Revision, Focus: trigger == "input"})
		shown = Shown{Win: b.win, ReqID: state.ReqID, Text: state.Text}
	})
	return shown
}

// stopWork is called under mu. Cancel functions never wait for UI work.
func (b *bubble) stopWork() {
	for _, cancel := range b.runCancels {
		cancel()
	}
	b.runCancels = nil
	if b.cancelCapture != nil {
		b.cancelCapture()
		b.cancelCapture = nil
	}
}

// Pin makes the bubble stay when it loses focus; the manager forgets it
// as the reusable one, so the next query opens a new window.
func (m *bubbleManager) Pin(id int64) {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		b, ok := m.byID[id]
		if !ok || b.pinned {
			m.mu.Unlock()
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
		if m.input == b {
			m.input = nil
		}
		m.pinned = append(m.pinned, b)
		m.mu.Unlock()
		m.refreshOutsideClick(b)
	})
}

// Unpin returns a pinned bubble to the transient life: it hides on blur
// again and becomes the bubble the next query reuses. The bubble it
// displaces is no longer the reusable one, and such leftovers destroy
// themselves once hidden (see the OnHide handler in create).
func (m *bubbleManager) Unpin(cfg config.Settings, id int64) {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		b, ok := m.byID[id]
		if !ok || !b.pinned {
			m.mu.Unlock()
			return
		}
		b.pinned = false
		if i := indexOfBubble(m.pinned, b); i >= 0 {
			m.pinned = append(m.pinned[:i], m.pinned[i+1:]...)
		}
		if b.trigger == "input" {
			m.input = b
		} else {
			m.current = b
		}
		if cfg.Bubble.AutoHide && b.offBlur == nil {
			b.offBlur = b.win.OnBlur(b.win.Hide)
		}
		m.mu.Unlock()
		m.refreshOutsideClick(b)
	})
}

// Close hides an unpinned bubble and closes a pinned one for good.
func (m *bubbleManager) Close(id int64, sessionID string, showID, revision int64) {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		b, ok := m.byID[id]
		ok = ok && b.claimClose(sessionID, showID, revision)
		if ok {
			b.stopWork()
			b.canShow = func() bool { return false }
		}
		m.mu.Unlock()
		if ok {
			b.win.Close()
		}
	})
}

// setAutoHide also updates visible bubbles displaced by Unpin.
func (m *bubbleManager) setAutoHide(on bool) {
	m.mu.Lock()
	var changed []*bubble
	for _, b := range m.byID {
		if b.pinned || (b.offBlur != nil) == on {
			continue
		}
		if on {
			b.offBlur = b.win.OnBlur(b.win.Hide)
		} else {
			b.offBlur()
			b.offBlur = nil
		}
		changed = append(changed, b)
	}
	m.mu.Unlock()
	for _, b := range changed {
		m.refreshOutsideClick(b)
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
	for _, b := range m.byID {
		out = append(out, b.win)
	}
	return out
}

func (m *bubbleManager) state(b *bubble, cfg config.Settings) BubbleState {
	return BubbleState{
		ID:                  b.id,
		ShowID:              b.showID,
		ClipboardRecoveryID: selection.PendingClipboardRecovery(),
		ReqID:               b.reqID,
		ServiceRequests:     cloneRequests(b.serviceRequests), Generation: b.generation, SessionID: b.sessionID, Revision: b.revision, Phase: b.phase, Message: b.message, Meta: b.meta, Results: b.resultList(),
		Trigger:    b.trigger,
		Text:       b.text,
		SourceLang: b.sourceLang,
		TargetLang: b.targetLang,
		Pinned:     b.pinned,
		Settings:   cfg,
	}
}

// showWin displays the bubble now, or as soon as its page is ready.
func (m *bubbleManager) showWin(b *bubble) {
	show := func() {
		m.mu.Lock()
		valid := b.canShow
		m.mu.Unlock()
		if valid != nil && !valid() {
			return
		}
		b.win.Show()
		m.refreshOutsideClick(b)
	}
	m.mu.Lock()
	if b.ready {
		m.mu.Unlock()
		mygo.RunOnMain(show)
		return
	}
	b.pendingShow = show
	m.mu.Unlock()
}

// Windows can deny foreground activation when a bubble shows. If it never
// acquires focus, an outside click must dismiss it without a blur event.
func (m *bubbleManager) refreshOutsideClick(b *bubble) {
	if runtime.GOOS != "windows" {
		return
	}
	mygo.RunOnMain(func() {
		b.stopOutsideClick()
		m.mu.Lock()
		needsWatch := b.offBlur != nil
		sessionID := b.sessionID
		m.mu.Unlock()
		if !needsWatch || !b.win.IsVisible() || b.win.IsFocused() {
			return
		}
		active := true // accessed only on the main thread
		stop, err := selection.WatchOutsideClick(b.win.NativeHandle(), func() {
			mygo.RunOnMain(func() {
				m.mu.Lock()
				dismiss := active && b.sessionID == sessionID && b.offBlur != nil
				m.mu.Unlock()
				if dismiss {
					b.win.Hide()
				}
			})
		})
		if err != nil {
			log.Printf("bubble outside click: %v", err)
			return
		}
		b.offOutsideClick = func() {
			active = false
			stop()
		}
	})
}

func (b *bubble) stopOutsideClick() {
	if b.offOutsideClick != nil {
		b.offOutsideClick()
		b.offOutsideClick = nil
	}
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
	// Once the bubble has focus, the blur listener can handle dismissal.
	b.win.OnFocus(b.stopOutsideClick)
	b.win.OnReadyToShow(func() {
		m.mu.Lock()
		b.ready = true
		show := b.pendingShow
		b.pendingShow = nil
		m.mu.Unlock()
		if show != nil {
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
		b.stopWork()
		b.reqID = ""
		b.serviceRequests = nil
		b.revision++
		b.closeClaimed = true
		b.fullRevision, b.fullReqID = b.revision, ""
		stopped := RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision}
		b.canShow = func() bool { return false }
		m.mu.Unlock()
		RunStopped.Emit(b.win, stopped)
		if quitting || pinned {
			return // close it for real
		}
		e.PreventDefault()
		b.win.Hide()
	})
	b.win.OnClosed(func() {
		b.stopOutsideClick()
		m.mu.Lock()
		defer m.mu.Unlock()
		b.stopWork()
		delete(m.byID, b.id)
		if m.input == b {
			m.input = nil
		}
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
		b.stopOutsideClick()
		m.mu.Lock()
		stale := !b.pinned && m.current != b && m.input != b
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

func (m *bubbleManager) hideTransient() {
	m.mu.Lock()
	input, current := m.input, m.current
	m.mu.Unlock()
	if input != nil {
		m.hideTransientBubble(input)
	}
	if current != nil {
		m.hideTransientBubble(current)
	}
}

func (m *bubbleManager) hideTransientBubble(b *bubble) {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		pinned := b.pinned
		m.mu.Unlock()
		if !pinned {
			b.win.Hide()
		}
	})
}
