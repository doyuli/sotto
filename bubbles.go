package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/selection"
)

type bubbleActions struct {
	run          func(win *mygo.Window, req RunReq)
	settings     func() config.Settings
	startOCR     func()
	openSettings func()
}

// bubbleManager keeps one reusable window for all entries. Pinning detaches
// it from reuse; pinned windows remain independent.
//
// Window creation, presentation, pin changes and Close commit on the UI thread.
// m.mu guards state shared with translation workers.
// Release m.mu before native window calls. See docs/translation-state.md for
// the identities and ordering rules used by backend state and frontend events.
type bubbleManager struct {
	mu        sync.Mutex
	nextID    int64
	transient *bubble
	byID      map[int64]*bubble
	actions   bubbleActions
	// isQuitting reports a quit under way: the close handlers then let
	// windows close instead of hiding, or the quit sequence would abort.
	isQuitting func() bool
}

type bubble struct {
	id                     int64
	win                    *mygo.Window
	view                   *bubbleView
	appliedHeight          int
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
	b := m.transient
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
	reuse := m.transient == b && m.byID[b.id] == b && b.refocusInput(visible)
	var presented BubblePresentedMsg
	if reuse {
		presented = BubblePresentedMsg{ClipboardRecoveryID: selection.PendingClipboardRecovery(), SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision, Focus: true}
	}
	m.mu.Unlock()
	if !reuse {
		return false
	}
	b.win.Focus()
	if b.view != nil {
		b.view.onPresented(presented)
		b.win.Invalidate()
	}
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

// ShowTask starts a fresh session in the shared transient window.
// valid is checked again on the UI thread, including a delayed first show.
// Call from a worker only, and check Shown.Win before starting follow-up work.
func (m *bubbleManager) ShowTask(cfg config.Settings, trigger, text string, anchor *Anchor, phase, message string, valid func() bool, cancel context.CancelFunc) Shown {
	var shown Shown
	// Presentation and close commit on the same UI thread. A delayed close
	// cannot pass validation and then race a new presentation before Hide.
	mygo.RunOnMain(func() {
		if valid != nil && !valid() {
			return
		}
		m.mu.Lock()
		b := m.transient
		m.mu.Unlock()
		if b == nil {
			b = m.createTransient(cfg)
			if valid != nil && !valid() {
				b.win.Destroy()
				return
			}
		}
		m.mu.Lock()
		b.replaceContent(cfg, trigger, text, phase, message, cancel)
		b.present()
		b.canShow = valid
		m.mu.Unlock()
		// Reset and measure before presenting the reused window.
		b.win.Hide()
		initialH := 200
		if text != "" {
			initialH = 240
		}
		m.mu.Lock()
		b.appliedHeight = initialH
		m.mu.Unlock()
		b.win.SetContentSize(480, initialH)
		m.position(b, trigger, anchor)
		m.reflow(b, true)
		m.mu.Lock()
		state := m.state(b, cfg)
		view := b.view
		m.mu.Unlock()
		if view != nil {
			view.applyState(state, trigger == "input")
			b.win.Invalidate()
		}
		shown = Shown{Win: b.win, ReqID: state.ReqID, Text: state.Text}
		m.showWin(b)
		if view == nil {
			m.watchLayout(b, state.SessionID, state.ShowID)
		}
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

// SetPinned moves a live presentation out of or back into the reuse slot.
func (m *bubbleManager) SetPinned(ctx context.Context, cfg config.Settings, win *mygo.Window, id int64, session string, showID int64, pinned bool) bool {
	accepted := false
	mygo.RunOnMain(func() {
		if ctx.Err() != nil || win == nil {
			return
		}
		b, displaced := m.changePin(cfg, win, id, session, showID, pinned)
		if b == nil {
			return
		}
		// OnHide cannot reclaim a window that was already hidden when Unpin
		// displaced it. Destroy it directly after invalidating its work.
		if displaced != nil {
			displaced.win.Destroy()
		}
		m.refreshWindowLeave(b)
		accepted = true
	})
	return accepted
}

// changePin commits ownership before any displaced native window is destroyed.
// It runs on the main thread and holds mu while revoking the old session.
func (m *bubbleManager) changePin(cfg config.Settings, win *mygo.Window, id int64, session string, showID int64, pinned bool) (b, displaced *bubble) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b = m.byID[id]
	if b == nil || b.win != win || b.sessionID != session || b.showID != showID || b.closeClaimed || !b.presentationShown || b.pinned == pinned {
		return nil, nil
	}
	b.pinned = pinned
	if pinned {
		m.transient = nil
		b.placement = nil // User-dragged windows keep their position after unpinning.
	} else {
		displaced = m.transient
		m.transient = b
		if displaced != nil {
			displaced.dismiss()
		}
	}
	m.setBlurLocked(b, cfg.Bubble.AutoHide && !pinned)
	return b, displaced
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

func (m *bubbleManager) setAutoHide(on bool) {
	m.mu.Lock()
	b := m.transient
	changed := b != nil && (b.offBlur != nil) != on
	if changed {
		m.setBlurLocked(b, on)
	}
	m.mu.Unlock()
	if changed {
		m.refreshWindowLeave(b)
	}
}

// Blur listener registration does not call the native window or run callbacks.
func (m *bubbleManager) setBlurLocked(b *bubble, on bool) {
	if on && b.offBlur == nil {
		b.offBlur = b.win.OnBlur(m.blurHandler(b))
	} else if !on && b.offBlur != nil {
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

// pushMeta delivers resolved request metadata to the bubble's native UI.
func (m *bubbleManager) pushMeta(win *mygo.Window, meta RunMetaMsg) {
	m.mu.Lock()
	var view *bubbleView
	for _, b := range m.byID {
		if b.win == win {
			view = b.view
			break
		}
	}
	m.mu.Unlock()
	if view != nil {
		win.Update(func() { view.applyMeta(meta) })
	}
}

// pushResult delivers one translation chunk/result to the bubble's native UI.
func (m *bubbleManager) pushResult(win *mygo.Window, msg TranslationUpdateMsg) {
	m.mu.Lock()
	var view *bubbleView
	for _, b := range m.byID {
		if b.win == win {
			view = b.view
			break
		}
	}
	m.mu.Unlock()
	if view != nil {
		win.Update(func() { view.applyResult(msg) })
	}
}

// applySettings updates every open bubble view with new settings.
func (m *bubbleManager) applySettings(s config.Settings) {
	m.mu.Lock()
	type target struct {
		win  *mygo.Window
		view *bubbleView
	}
	var list []target
	for _, b := range m.byID {
		if b.win != nil && b.view != nil {
			list = append(list, target{win: b.win, view: b.view})
		}
	}
	m.mu.Unlock()
	for _, item := range list {
		v := item.view
		item.win.Update(func() { v.applySettings(s) })
	}
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

// createTransient fills the empty reuse slot on the main thread with a native
// ui.View bubble window, avoiding WebView2 startup and per-bubble memory cost.
func (m *bubbleManager) createTransient(cfg config.Settings) *bubble {
	id := m.nextID
	m.nextID++
	b := &bubble{id: id, maxHeight: bubbleMaxHeight, appliedHeight: 240}
	b.view = newBubbleView(id, cfg, bubbleViewActions{
		run: func(req RunReq) {
			if m.actions.run != nil && b.win != nil {
				m.actions.run(b.win, req)
			}
		},
		cancel: func(sessionID string, revision int64) {
			m.Cancel(b.win, sessionID, revision)
		},
		setPinned: func(sessionID string, showID int64, pinned bool) bool {
			currentCfg := cfg
			if m.actions.settings != nil {
				currentCfg = m.actions.settings()
			}
			return m.SetPinned(context.Background(), currentCfg, b.win, b.id, sessionID, showID, pinned)
		},
		close: func(sessionID string, showID, revision int64) bool {
			return m.Close(b.id, sessionID, showID, revision)
		},
		startOCR: func() {
			if m.actions.startOCR != nil {
				m.actions.startOCR()
			}
		},
		openSettings: func() {
			if m.actions.openSettings != nil {
				m.actions.openSettings()
			}
		},
		restoreClipboard: recoverClipboard,
		discardClipboard: selection.DiscardClipboardRecovery,
		copyText: func(_ *ui.Context, text string) error {
			if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
				return fmt.Errorf("复制失败: %w", err)
			}
			return nil
		},
		onHeightMeasured: func(height float64) {
			m.mu.Lock()
			limit := b.maxHeight
			prevH := b.appliedHeight
			closed := b.closeClaimed
			m.mu.Unlock()
			if closed {
				return
			}
			targetH := bubbleLayoutHeight(height, limit, 1)
			if targetH == prevH {
				return
			}
			m.mu.Lock()
			b.appliedHeight = targetH
			m.mu.Unlock()
			m.reflow(b, false)
			b.win.SetContentSize(480, targetH)
			m.reflow(b, true)
		},
	})
	b.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "Sotto",
		Content:         ui.View(b.view.render),
		Width:           480,
		Height:          240,
		MinWidth:        480,
		MaxWidth:        480,
		MinHeight:       160,
		MaxHeight:       760,
		DisableResize:   true, // the bubble has a fixed width
		Hidden:          true, // shown once placed and ready
		Frameless:       true,
		Panel:           true, // floats over every app, out of the taskbar
		SkipTaskbar:     true,
		CornerRadius:    14,
		BackgroundColor: "light-dark(#ffffff, #222325)",
	})
	b.win.OnResize(func() { m.reflow(b, true) })
	b.win.OnMove(func() { m.reflow(b, false) })
	b.win.OnReadyToShow(func() {
		log.Printf("bubble: view ready id=%d", b.id)
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
		view := b.view
		m.mu.Unlock()
		if view != nil {
			view.onStopped(stopped)
			b.win.Invalidate()
		}
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
		if m.transient == b {
			m.transient = nil
		}
	})
	b.win.OnHide(b.stopWindowLeave)
	m.mu.Lock()
	m.setBlurLocked(b, cfg.Bubble.AutoHide)
	m.byID[id] = b
	m.transient = b
	m.mu.Unlock()
	return b
}

func recoverClipboard(id string) error {
	err := selection.RecoverClipboard(id)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, selection.ErrSourceChanged):
		return errors.New("你已复制了新内容，本次没有覆盖它。可放弃恢复之前的内容，然后继续取词。")
	case errors.Is(err, selection.ErrClipboardRecoveryGone):
		return errors.New("之前的剪贴板内容已恢复或已放弃恢复，请重新打开面板。")
	default:
		log.Printf("clipboard recovery: %v", err)
		return errors.New("暂时无法恢复剪贴板，之前的内容仍在，可稍后重试。")
	}
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

func (m *bubbleManager) hideTransient() {
	m.mu.Lock()
	b := m.transient
	m.mu.Unlock()
	if b != nil {
		m.hideTransientBubble(b)
	}
}

// Semantic dismissal, unlike ShowTask's internal layout Hide, revokes all work
// before native events can deliver late requests or results for this session.
func (m *bubbleManager) hideTransientBubble(b *bubble) {
	mygo.RunOnMain(func() {
		m.mu.Lock()
		dismissed := m.byID[b.id] == b && b.dismiss()
		stopped := RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision}
		view := b.view
		m.mu.Unlock()
		if dismissed {
			if view != nil {
				view.onStopped(stopped)
				b.win.Invalidate()
			}
			b.win.Hide()
		}
	})
}
