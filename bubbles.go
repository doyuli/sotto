package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/selection"
)

// bubbleManager keeps separate reusable input and capture windows.
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
	// Showing requires both native page readiness and the current content layout.
	ready             bool
	awaitingLayout    bool
	presentationShown bool
	layoutWaitLogged  bool
	layoutID          int64
	layoutSequence    int64
	offBlur           func() // unregisters focus hiding; Pin calls it
	// offWindowLeave belongs to the main thread, like the window it watches.
	offWindowLeave func()

	// Placement and its reentrancy guard belong to the main thread.
	placement       *bubblePlacement
	adjustingLayout bool
	maxHeight       float64 // CSS pixels; guarded by the manager mutex
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

// focusVisibleInput runs on the UI thread before a new entry is acquired.
// A pending selection can leave input visible, so visibility alone is not reuse.
func (m *bubbleManager) focusVisibleInput() bool {
	m.mu.Lock()
	b := m.input
	var valid func() bool
	if b != nil {
		valid = b.canShow
	}
	m.mu.Unlock()
	if b == nil || valid == nil || !valid() {
		return false
	}
	visible := b.win.IsVisible()
	m.mu.Lock()
	reuse := m.input == b && m.byID[b.id] == b && b.refocusInput(visible)
	var presented BubblePresentedMsg
	if reuse {
		presented = BubblePresentedMsg{ClipboardRecoveryID: selection.PendingClipboardRecovery(), SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision, Focus: true}
	}
	m.mu.Unlock()
	if !reuse {
		return false
	}
	b.win.Focus()
	BubblePresented.Emit(b.win, presented)
	m.refreshWindowLeave(b)
	log.Printf("bubble: focus existing input id=%d show=%d", b.id, presented.ShowID)
	return true
}

// refocusInput preserves content and first-show state while revoking callbacks
// from the previous presentation. Entry ownership is checked separately.
func (b *bubble) refocusInput(visible bool) bool {
	if !visible || b.trigger != "input" || b.pinned || b.closeClaimed || !b.presentationShown || b.awaitingLayout {
		return false
	}
	b.showID++
	return true
}

// ShowTask starts a fresh session in the input or capture window.
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
		if trigger == "input" {
			m.input = b
		} else {
			m.current = b
		}
		b.replaceContent(cfg, trigger, text, phase, message, cancel)
		b.present()
		b.canShow = valid
		m.mu.Unlock()
		// Reset and measure while hidden: the reused window still has its old size.
		b.win.Hide()
		m.position(b, trigger, anchor)
		m.reflow(b, true)
		m.mu.Lock()
		state := m.state(b, cfg)
		m.mu.Unlock()
		BubbleReset.Emit(b.win, state)
		BubblePresented.Emit(b.win, BubblePresentedMsg{ClipboardRecoveryID: state.ClipboardRecoveryID, SessionID: state.SessionID, ShowID: state.ShowID, Revision: state.Revision, Focus: trigger == "input"})
		shown = Shown{Win: b.win, ReqID: state.ReqID, Text: state.Text}
		m.showWin(b)
		m.watchLayout(b, state.SessionID, state.ShowID)
	})
	return shown
}

// A timeout diagnoses missing acknowledgements without exposing a reused page's
// old DOM. The page owns bounded retries; only a current layout may release show.
func (m *bubbleManager) watchLayout(b *bubble, session string, showID int64) {
	time.AfterFunc(400*time.Millisecond, func() {
		mygo.RunOnMain(func() {
			m.mu.Lock()
			valid := b.canShow
			waiting := m.byID[b.id] == b && b.sessionID == session && b.showID == showID && !b.closeClaimed && !b.presentationShown && b.awaitingLayout
			ready, layoutID, sequence := b.ready, b.layoutID, b.layoutSequence
			m.mu.Unlock()
			if waiting && (valid == nil || valid()) {
				log.Printf("bubble: layout timeout id=%d show=%d layout=%d sequence=%d ready=%t", b.id, showID, layoutID, sequence, ready)
			}
		})
	})
}

// stopWork is called under mu. Cancel functions never wait for UI work.
func (b *bubble) stopWork() {
	for _, cancel := range b.runCancels {
		cancel()
	}
	b.runCancels = nil
	// A later Ready snapshot must not revive an already canceled stream.
	for service, result := range b.results {
		if result.Streaming {
			result.Streaming, result.Err = false, "翻译已取消，请重试"
			b.results[service] = result
		}
	}
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
		b.placement = nil // User-dragged windows keep their position after unpinning.
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
		m.refreshWindowLeave(b)
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
			b.offBlur = b.win.OnBlur(m.blurHandler(b))
		}
		m.mu.Unlock()
		m.refreshWindowLeave(b)
	})
}

// Close hides an unpinned bubble and closes a pinned one for good.
func (m *bubbleManager) Close(id int64, sessionID string, showID, revision int64) bool {
	accepted := false
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
			accepted = true
		}
	})
	return accepted
}

// Blur can be delivered while Show/Hide is changing activation. Defer the
// decision until that native call finishes, then check the actual focus and
// presentation identity instead of letting an old blur hide the next result.
func (m *bubbleManager) blurHandler(b *bubble) func() {
	return func() {
		m.mu.Lock()
		showID := b.showID
		m.mu.Unlock()
		go mygo.RunOnMain(func() {
			m.hideAfterBlur(b, showID)
		})
	}
}

// hideAfterBlur runs on the main thread, including for foreground notifications
// from an owned popup that does not deliver another blur to the bubble itself.
func (m *bubbleManager) hideAfterBlur(b *bubble, showID int64) {
	visible, focused := b.win.IsVisible(), b.win.IsFocused()
	if runtime.GOOS == "windows" && !focused {
		focused = selection.WindowOwnsForeground(b.win.NativeHandle())
	}
	m.mu.Lock()
	hide := b.shouldHideAfterBlur(showID, visible, focused)
	m.mu.Unlock()
	if hide {
		log.Printf("bubble: blur hide id=%d show=%d", b.id, showID)
		m.hideTransientBubble(b)
	}
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
			b.offBlur = b.win.OnBlur(m.blurHandler(b))
		} else {
			b.offBlur()
			b.offBlur = nil
		}
		changed = append(changed, b)
	}
	m.mu.Unlock()
	for _, b := range changed {
		m.refreshWindowLeave(b)
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
		LayoutID:            b.layoutID,
		LayoutSequence:      b.layoutSequence,
		MaxHeight:           b.maxHeight,
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

// showWin runs on the UI thread. Native readiness alone says nothing about
// whether a reused page has rendered its replacement session.
func (m *bubbleManager) showWin(b *bubble) {
	m.mu.Lock()
	valid := b.canShow
	m.mu.Unlock()
	if valid != nil && !valid() {
		return
	}
	m.mu.Lock()
	show := b.claimShow()
	waiting := !show && b.awaitingLayout && !b.closeClaimed && !b.presentationShown && b.showID != 0 && !b.layoutWaitLogged
	if waiting {
		b.layoutWaitLogged = true
	}
	ready, showID, layoutID, sequence := b.ready, b.showID, b.layoutID, b.layoutSequence
	m.mu.Unlock()
	if waiting {
		log.Printf("bubble: show waiting for layout id=%d show=%d layout=%d sequence=%d ready=%t", b.id, showID, layoutID, sequence, ready)
	}
	if !show {
		return
	}
	b.win.Show()
	log.Printf("bubble: show id=%d show=%d visible=%t focused=%t", b.id, b.showID, b.win.IsVisible(), b.win.IsFocused())
	m.refreshWindowLeave(b)
}

// claimShow commits each first presentation once under mu. Further content
// measurements resize hidden windows without reopening a dismissed bubble.
func (b *bubble) claimShow() bool {
	if !b.ready || b.awaitingLayout || b.closeClaimed || b.showID == 0 || b.presentationShown {
		return false
	}
	b.presentationShown = true
	return true
}

// Keep watching while focused: an owned popup can take activation, then lose it
// to another app without delivering a second blur to the bubble. Outside clicks
// also dismiss bubbles that Windows never allowed to acquire foreground focus.
func (m *bubbleManager) refreshWindowLeave(b *bubble) {
	if runtime.GOOS != "windows" {
		return
	}
	mygo.RunOnMain(func() {
		b.stopWindowLeave()
		m.mu.Lock()
		needsWatch := b.offBlur != nil
		sessionID, showID := b.sessionID, b.showID
		m.mu.Unlock()
		if !needsWatch || !b.win.IsVisible() {
			return
		}
		active := true // accessed only on the main thread
		notify := func() {
			mygo.RunOnMain(func() {
				m.mu.Lock()
				dismiss := active && b.sessionID == sessionID && b.showID == showID && b.offBlur != nil
				m.mu.Unlock()
				if dismiss {
					// A select popup may have closed before the mouse hit test
					// ran. Recheck current focus for both kinds of notification.
					m.hideAfterBlur(b, showID)
				}
			})
		}
		stop, err := selection.WatchWindowLeave(b.win.NativeHandle(), notify)
		if err != nil {
			log.Printf("bubble window watch: %v", err)
			return
		}
		b.offWindowLeave = func() {
			active = false
			stop()
		}
	})
}

func (b *bubble) stopWindowLeave() {
	if b.offWindowLeave != nil {
		b.offWindowLeave()
		b.offWindowLeave = nil
	}
}

// create makes a hidden bubble window. It runs outside m.mu — creating
// a window waits for the main thread — before hotkeys are registered or
// under showMu, so no two bubbles are ever created at once.
func (m *bubbleManager) create(cfg config.Settings) *bubble {
	id := m.nextID
	m.nextID++
	b := &bubble{id: id, maxHeight: bubbleMaxHeight}
	b.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "Sotto",
		URL:             fmt.Sprintf("/bubble.html?b=%d", id),
		Width:           480,
		Height:          240,
		MinWidth:        480,
		MaxWidth:        480,
		MinHeight:       160,
		MaxHeight:       760,
		DisableResize:   true,  // the bubble has a fixed width
		FitContent:      false, // only ordered app measurements may resize the bubble
		Hidden:          true,  // shown once placed and ready
		Frameless:       true,
		Panel:           true, // floats over every app, out of the taskbar
		SkipTaskbar:     true,
		Transparent:     true, // the page draws its rounded card
		CornerRadius:    14,
		BackgroundColor: "light-dark(#ffffff, #222325)",
		Page:            mygo.PageOptions{DevTools: mygo.DevToolsAuto},
	})
	b.win.OnResize(func() { m.reflow(b, true) })
	b.win.OnMove(func() { m.reflow(b, false) })
	b.win.OnReadyToShow(func() {
		log.Printf("bubble: page ready id=%d", b.id)
		m.mu.Lock()
		b.ready = true
		m.mu.Unlock()
		m.showWin(b)
	})
	b.win.OnClose(func(e *mygo.CloseEvent) {
		// The app quitting closes its windows for real: mygo's quit
		// sequence aborts when a window refuses to close, and a bubble
		// hiding instead would keep the app alive forever.
		quitting := m.isQuitting != nil && m.isQuitting()
		m.mu.Lock()
		pinned := b.pinned
		stopped := b.stopPresentation()
		m.mu.Unlock()
		RunStopped.Emit(b.win, stopped)
		if quitting || pinned {
			return // close it for real
		}
		e.PreventDefault()
		b.win.Hide()
	})
	b.win.OnClosed(func() {
		b.stopWindowLeave()
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
		b.stopWindowLeave()
		m.mu.Lock()
		stale := !b.pinned && m.current != b && m.input != b
		m.mu.Unlock()
		if stale {
			b.win.Destroy()
		}
	})
	if cfg.Bubble.AutoHide {
		b.offBlur = b.win.OnBlur(m.blurHandler(b))
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
	point := mygo.Screen.CursorScreenPoint()
	if anchor != nil {
		point = mygo.Point{X: anchor.X, Y: anchor.Y}
	}
	display := mygo.Screen.DisplayNearestPoint(point)
	width, height := b.win.Size()
	placement := newBubblePlacement(display, point, trigger == "input", width)
	b.placement = &placement
	x, y := placement.position(width, height)
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

// Semantic dismissal, unlike ShowTask's internal layout Hide, revokes all work
// before native events can deliver late requests or results for this session.
func (m *bubbleManager) hideTransientBubble(b *bubble) {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		dismissed := m.byID[b.id] == b && b.dismiss()
		stopped := RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision}
		m.mu.Unlock()
		if dismissed {
			RunStopped.Emit(b.win, stopped)
			b.win.Hide()
		}
	})
}
