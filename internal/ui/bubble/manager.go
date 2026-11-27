package bubble

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"runtime"
	"slices"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/platform"
	"sotto/internal/selection"
	"sotto/internal/usermessage"
)

type Actions struct {
	Speak           func(text, lang string, onError func(error))
	Run             func(win *mygo.Window, req RunReq)
	Settings        func() config.Settings
	StartOCR        func()
	OpenSettings    func(source *mygo.Window)
	OnInputAutoHide func(selection.PrimaryPress)
}

// Manager keeps one reusable window for all entries. Pinning detaches
// it from reuse; pinned windows remain independent.
//
// Window creation, presentation, pin changes and Close commit on the UI thread.
// m.mu guards state shared with translation workers.
// Release m.mu before native window calls. See docs/translation-state.md for
// the identities and ordering rules used by backend state and native view updates.
type Manager struct {
	mu        sync.Mutex
	nextID    int64
	transient *bubble
	byID      map[int64]*bubble
	actions   Actions
	// isQuitting reports a quit under way: the close handlers then let
	// windows close instead of hiding, or the quit sequence would abort.
	isQuitting func() bool
}

type bubble struct {
	id                     int64
	win                    *mygo.Window
	view                   *bubbleView
	pinned                 bool
	reqID                  string // request with the highest accepted revision
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
	runCancels             map[string]context.CancelFunc
	serviceRequests        map[string]string
	cancelCapture          context.CancelFunc
	canShow                func() bool
	// Restoring a fixed bubble renews its workspace, never its entry ownership.
	workspace uint64
	// Source content initializes the view before translation starts.
	trigger, text string
	// Native readiness gates first presentation; the view owns content measurement.
	ready             bool
	presentationShown bool
	offBlur           func() // unregisters focus hiding; Pin calls it
	// offWindowLeave belongs to the main thread, like the window it watches.
	offWindowLeave func()

	// Placement, observed geometry and its reentrancy guard belong to the main thread.
	placement       *bubblePlacement
	positionState   bubblePosition
	adjustingLayout bool
	keepTop         bool
	heightAnimation *bubbleHeightAnimation
	maxHeight       float64 // native logical units; guarded by the manager mutex
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

func NewManager(isQuitting func() bool, actions Actions) *Manager {
	return &Manager{byID: map[int64]*bubble{}, isQuitting: isQuitting, actions: actions}
}

// FocusVisibleInput runs on the UI thread before a new entry is acquired.
// A pending selection can leave input visible, so visibility alone is not reuse.
func (m *Manager) FocusVisibleInput() bool {
	b, visible := m.visibleInputCandidate()
	if b == nil {
		return false
	}
	m.mu.Lock()
	reuse := m.transient == b && m.byID[b.id] == b && b.refocusInput(visible)
	var presented BubblePresentedMsg
	if reuse {
		presented = BubblePresentedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision, Focus: true}
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

// HideVisibleInput closes a visible transient input bubble when the panel
// shortcut is pressed again, returning true when an active input was dismissed.
func (m *Manager) HideVisibleInput() bool {
	b, visible := m.visibleInputCandidate()
	if b == nil {
		return false
	}
	m.mu.Lock()
	dismissed := m.transient == b && m.byID[b.id] == b && b.dismissInput(visible)
	stopped := RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision}
	view := b.view
	m.mu.Unlock()
	if !dismissed {
		return false
	}
	b.hideStopped(view, stopped)
	log.Printf("bubble: toggle hide input id=%d show=%d", b.id, stopped.ShowID)
	return true
}

func (m *Manager) visibleInputCandidate() (*bubble, bool) {
	m.mu.Lock()
	b := m.transient
	var valid func() bool
	if b != nil {
		valid = b.canShow
	}
	m.mu.Unlock()
	if b == nil || valid == nil || !valid() {
		return nil, false
	}
	return b, b.win.IsVisible()
}

func (b *bubble) isVisibleInput(visible bool) bool {
	return visible && b.trigger == "input" && !b.pinned && !b.closeClaimed && b.presentationShown
}

// refocusInput preserves content and first-show state while revoking callbacks
// from the previous presentation. Entry ownership is checked separately.
func (b *bubble) refocusInput(visible bool) bool {
	if !b.isVisibleInput(visible) {
		return false
	}
	b.showID++
	return true
}

func (b *bubble) dismissInput(visible bool) bool {
	if !b.isVisibleInput(visible) {
		return false
	}
	return b.dismiss()
}

// ShowTask starts a fresh session in the shared transient window.
// valid is checked again on the UI thread, including a delayed first show.
// Call from a worker only, and check Shown.Win before starting follow-up work.
func (m *Manager) ShowTask(cfg config.Settings, trigger, text string, anchor *Anchor, phase, message string, valid func() bool, cancel context.CancelFunc) Shown {
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
		b.workspace = platform.WorkspaceGeneration()
		m.mu.Unlock()
		// Reset geometry and native view state before presenting the reused window.
		b.stopHeightAnimation()
		b.win.Hide()
		b.keepTop = false
		initialH := 200
		if text != "" {
			initialH = 240
		}
		b.setContentSize(480, initialH)
		m.position(b, trigger, anchor)
		m.reflow(b, true, 0)
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

// SetPinned moves a live presentation out of or back into the reuse slot.
func (m *Manager) SetPinned(ctx context.Context, cfg config.Settings, win *mygo.Window, id int64, session string, showID int64, pinned bool) bool {
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
func (m *Manager) changePin(cfg config.Settings, win *mygo.Window, id int64, session string, showID int64, pinned bool) (b, displaced *bubble) {
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
func (m *Manager) Close(id int64, sessionID string, showID, revision int64) bool {
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
func (m *Manager) blurHandler(b *bubble) func() {
	return func() {
		m.mu.Lock()
		showID := b.showID
		m.mu.Unlock()
		press := selection.CapturePrimaryPress()
		go mygo.RunOnMain(func() {
			m.hideAfterBlur(b, showID, press)
		})
	}
}

// hideAfterBlur runs on the main thread, including for foreground notifications
// from an owned popup that does not deliver another blur to the bubble itself.
func (m *Manager) hideAfterBlur(b *bubble, showID int64, press selection.PrimaryPress) {
	visible, focused := b.win.IsVisible(), selection.WindowHasFocus(b.win)
	m.mu.Lock()
	hide := b.shouldHideAfterBlur(showID, visible, focused)
	m.mu.Unlock()
	if hide {
		log.Printf("bubble: blur hide id=%d show=%d", b.id, showID)
		m.hideTransientBubble(b, press)
	}
}

func (m *Manager) SetAutoHide(on bool) {
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
func (m *Manager) setBlurLocked(b *bubble, on bool) {
	if on && b.offBlur == nil {
		b.offBlur = b.win.OnBlur(m.blurHandler(b))
	} else if !on && b.offBlur != nil {
		b.offBlur()
		b.offBlur = nil
	}
}

// ApplySettings updates every open bubble view with new settings.
func (m *Manager) ApplySettings(s config.Settings) {
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
		item.win.Update(func() { v.ApplySettings(s) })
	}
}

func (m *Manager) state(b *bubble, cfg config.Settings) BubbleState {
	return BubbleState{
		ID:         b.id,
		MaxHeight:  b.maxHeight,
		ShowID:     b.showID,
		ReqID:      b.reqID,
		Generation: b.generation,
		SessionID:  b.sessionID,
		Revision:   b.revision,
		Phase:      b.phase,
		Message:    b.message,
		Trigger:    b.trigger,
		Text:       b.text,
		SourceLang: b.sourceLang,
		TargetLang: b.targetLang,
		Pinned:     b.pinned,
		Settings:   cfg,
	}
}

// showWin runs on the UI thread after applying the current native view state.
// Entry ownership is rechecked when native readiness delays first presentation.
func (m *Manager) showWin(b *bubble) {
	m.mu.Lock()
	valid := b.canShow
	m.mu.Unlock()
	if valid != nil && !valid() {
		return
	}
	m.mu.Lock()
	show := b.claimShow()
	m.mu.Unlock()
	if !show {
		return
	}
	platform.ShowBubbleWindow(b.win, b.trigger)
	log.Printf("bubble: show id=%d show=%d visible=%t focused=%t", b.id, b.showID, b.win.IsVisible(), b.win.IsFocused())
	m.refreshWindowLeave(b)
}

// claimShow commits each first presentation once under mu. Further content
// measurements resize hidden windows without reopening a dismissed bubble.
func (b *bubble) claimShow() bool {
	if !b.ready || b.closeClaimed || b.showID == 0 || b.presentationShown {
		return false
	}
	b.presentationShown = true
	return true
}

// Keep watching while focused: an owned popup can take activation, then lose it
// to another app without delivering a second blur to the bubble. Outside clicks
// also dismiss bubbles that Windows never allowed to acquire foreground focus.
func (m *Manager) refreshWindowLeave(b *bubble) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
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
		notify := func(press selection.PrimaryPress) {
			mygo.RunOnMain(func() {
				m.mu.Lock()
				dismiss := active && b.sessionID == sessionID && b.showID == showID && b.offBlur != nil
				m.mu.Unlock()
				if dismiss {
					if runtime.GOOS == "darwin" {
						// A click in the menu bar may leave a nonactivating panel
						// key. The native monitor already excluded owned menus.
						m.hideTransientBubble(b, press)
					} else {
						// A select popup may have closed since the hook hit test.
						m.hideAfterBlur(b, showID, press)
					}
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
func (m *Manager) createTransient(cfg config.Settings) *bubble {
	id := m.nextID
	m.nextID++
	b := &bubble{id: id, maxHeight: bubbleMaxHeight}
	b.view = newBubbleView(id, cfg, bubbleViewActions{
		run: func(req RunReq) {
			if m.actions.Run != nil && b.win != nil {
				// Source preparation and credential lookup must not hold a UI build.
				go m.actions.Run(b.win, req)
			}
		},
		cancel: func(sessionID string, revision int64) {
			m.Cancel(b.win, sessionID, revision)
		},
		setPinned: func(sessionID string, showID int64, pinned bool) bool {
			currentCfg := cfg
			if m.actions.Settings != nil {
				currentCfg = m.actions.Settings()
			}
			return m.SetPinned(context.Background(), currentCfg, b.win, b.id, sessionID, showID, pinned)
		},
		close: func(sessionID string, showID, revision int64) bool {
			return m.Close(b.id, sessionID, showID, revision)
		},
		startOCR: func() {
			if m.actions.StartOCR != nil {
				m.actions.StartOCR()
			}
		},
		openSettings: func() {
			if m.actions.OpenSettings != nil {
				m.actions.OpenSettings(b.win)
			}
		},
		copyText: func(_ *ui.Context, text string) error {
			if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
				log.Printf("write clipboard: %v", err)
				return usermessage.Explain(err, "请稍后重试")
			}
			return nil
		},
		speakText: func(text, lang string, onError func()) {
			if m.actions.Speak == nil {
				onError()
				return
			}
			m.actions.Speak(text, lang, func(err error) {
				log.Printf("speech: %v", err)
				b.win.Update(onError)
			})
		},
		onHeightMeasured: func(measurement bubbleHeightMeasurement) {
			session := b.view.sessionID
			// AppKit can redraw during a resize. Apply the measurement after
			// this build returns, while it still belongs to the same session.
			b.win.Update(func() {
				m.mu.Lock()
				current := m.byID[b.id] == b && !b.closeClaimed && b.sessionID == session
				m.mu.Unlock()
				if current {
					m.resizeToContent(b, measurement)
				}
			})
		},
	})
	b.win = mygo.NewWindow(mygo.WindowOptions{
		Title:           "Sotto",
		Content:         ui.View(b.view.render),
		Width:           480,
		Height:          240,
		MinWidth:        480,
		MaxWidth:        480,
		MinHeight:       bubbleMinHeight,
		MaxHeight:       bubbleMaxHeight,
		DisableResize:   true, // the bubble has a fixed width
		Hidden:          true, // shown once placed and ready
		Frameless:       true,
		Panel:           true, // floats over every app, out of the taskbar
		SkipTaskbar:     true,
		CornerRadius:    14,
		BackgroundColor: "light-dark(#ffffff, #222325)",
	})
	b.win.OnResize(func() { m.reflow(b, true, 0) })
	b.win.OnMove(func() { m.reflow(b, false, 0) })
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
		b.stopHeightAnimation()
		b.stopWindowLeave()
		m.mu.Lock()
		defer m.mu.Unlock()
		b.stopWork()
		delete(m.byID, b.id)
		if m.transient == b {
			m.transient = nil
		}
	})
	b.win.OnHide(func() {
		if animation := b.heightAnimation; animation != nil {
			b.stopHeightAnimation()
			// A pinned bubble may be restored before its hidden view redraws.
			m.reflow(b, true, animation.target)
		}
		b.stopWindowLeave()
	})
	m.mu.Lock()
	m.setBlurLocked(b, cfg.Bubble.AutoHide)
	m.byID[id] = b
	m.transient = b
	m.mu.Unlock()
	return b
}

// position puts the bubble near where the user is: beside an anchor
// when the grab could see the selection, beside the pointer for an OCR
// shot, and an input bubble a quarter of the way down its display.
func (m *Manager) position(b *bubble, trigger string, anchor *Anchor) {
	point := mygo.Screen.CursorScreenPoint()
	if anchor != nil {
		point = mygo.Point{X: anchor.X, Y: anchor.Y}
	}
	display := mygo.Screen.DisplayNearestPoint(point)
	width, height := b.win.Size()
	placement := newBubblePlacement(display, point, trigger == "input", width)
	b.placement = &placement
	x, y := placement.position(width, height)
	b.setPosition(x, y)
}

func (m *Manager) HideTransient() {
	m.mu.Lock()
	b := m.transient
	m.mu.Unlock()
	if b != nil {
		m.hideTransientBubble(b, selection.PrimaryPress{})
	}
}

// Semantic dismissal, unlike ShowTask's internal layout Hide, revokes all work
// before native events can deliver late requests or results for this session.
func (m *Manager) hideTransientBubble(b *bubble, press selection.PrimaryPress) {
	mygo.RunOnMain(func() {
		m.commitTransientBubbleHide(b, press, b.hideStopped)
	})
}

func (m *Manager) commitTransientBubbleHide(b *bubble, press selection.PrimaryPress, hide func(*bubbleView, RunStoppedMsg)) {
	m.mu.Lock()
	trigger := b.trigger
	dismissed := m.byID[b.id] == b && b.dismiss()
	stopped := RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision}
	view := b.view
	onAutoHide := m.actions.OnInputAutoHide
	m.mu.Unlock()
	if dismissed {
		if !press.At.IsZero() && trigger == "input" && onAutoHide != nil {
			onAutoHide(press)
		}
		hide(view, stopped)
	}
}

// Called after releasing the manager lock, on the UI thread.
func (b *bubble) hideStopped(view *bubbleView, stopped RunStoppedMsg) {
	if view != nil {
		view.onStopped(stopped)
		b.win.Invalidate()
	}
	b.win.Hide()
}

// HideStale hides presentations revoked by the current workspace generation.
// It runs on the UI thread; no lock is held across native window operations.
func (m *Manager) HideStale() {
	workspace := platform.WorkspaceGeneration()
	m.mu.Lock()
	var stale []*bubble
	for _, b := range m.byID {
		if b.stopStalePresentation(workspace) {
			stale = append(stale, b)
		}
	}
	m.mu.Unlock()
	for _, b := range stale {
		if !b.pinned && b.view != nil {
			b.view.onStopped(RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision})
			b.win.Invalidate()
		}
		b.win.Hide()
	}
}

// Fixed bubbles retain their independent jobs across workspace changes.
func (b *bubble) stopStalePresentation(workspace uint64) bool {
	if b.workspace == workspace {
		return false
	}
	if !b.pinned {
		return b.dismiss()
	}
	return true
}

// A retained menu action must not revive a closed or unpinned window.
func (m *Manager) claimPinnedRestore(b *bubble, workspace uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID[b.id] != b || !b.pinned || b.closeClaimed {
		return false
	}
	b.workspace = workspace
	return true
}

// PinnedRestoreMenuItem lists hidden pinned bubbles in creation order.
func (m *Manager) PinnedRestoreMenuItem(refresh func()) *mygo.MenuItem {
	m.mu.Lock()
	var pinned []*bubble
	for _, b := range m.byID {
		if b.pinned && !b.closeClaimed {
			pinned = append(pinned, b)
		}
	}
	m.mu.Unlock()
	slices.SortFunc(pinned, func(a, b *bubble) int { return cmp.Compare(a.id, b.id) })
	var items []*mygo.MenuItem
	for _, b := range pinned {
		if !b.win.IsVisible() {
			items = append(items, &mygo.MenuItem{Label: fmt.Sprintf("固定气泡 %d", b.id+1), Click: func(*mygo.MenuItem, *mygo.Window) {
				if m.claimPinnedRestore(b, platform.WorkspaceGeneration()) {
					b.win.Show()
				}
				refresh()
			}})
		}
	}
	switch len(items) {
	case 0:
		return nil
	case 1:
		items[0].Label = "恢复" + items[0].Label
		return items[0]
	default:
		return &mygo.MenuItem{Label: "恢复固定气泡", Submenu: items}
	}
}
