package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/langdetect"
	"sotto/internal/naming"
	"sotto/internal/ocr"
	"sotto/internal/selection"
	"sotto/internal/translate"
)

//go:embed resources/icon.png
var iconPNG []byte

// The hotkey actions, and the accelerators to fall back to when the
// configured one is taken. Alt+D is the browser address bar's shortcut,
// among others, so a busy default must not leave the action dead.
var hotkeyActions = []string{"panel", "selection", "ocr"}

var hotkeyFallbacks = map[string][]string{
	"panel":     {"Alt+A", "Alt+Q", "Ctrl+Alt+Q"},
	"selection": {"Alt+D", "Alt+E", "Ctrl+Alt+E"},
	"ocr":       {"Alt+S", "Alt+W", "Ctrl+Alt+W"},
}

var hotkeyLabels = map[string]string{
	"panel":     "翻译面板",
	"selection": "划词翻译",
	"ocr":       "截图翻译",
}

// application is the app's state: the settings, the credential store,
// the bubble windows and the settings window. Its mutex guards the
// fields and is never held across a call that waits for the main thread.
type application struct {
	cfgPath string

	mu          sync.Mutex
	cfg         config.Settings
	hotkeys     map[string]string // action → the accelerator in force
	runs        map[string]context.CancelFunc
	settingsWin *mygo.Window
	quitting    bool
	// autoGrab keeps the double-click watch's grabs one at a time.
	autoGrab atomic.Bool

	bubbles *bubbleManager
	snip    *snipManager
	tray    *mygo.Tray
	keys    keys.Store
}

func newApplication() *application {
	return &application{keys: keys.Store{}, runs: map[string]context.CancelFunc{}, snip: &snipManager{}}
}

// isQuitting reports whether a quit is under way. The bubbles' OnClose
// handlers let their close through then: mygo's quit sequence closes
// every window and aborts as soon as one prevents its close, so a bubble
// hiding instead of closing would keep the app from ever exiting.
func (a *application) isQuitting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quitting
}

// ready assembles the app once mygo is up: settings, bubbles, hotkeys
// and the tray. It runs on the main thread.
func (a *application) ready() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("user data path: %v", err)
		dir = "."
	}
	a.cfgPath = filepath.Join(dir, "settings.json")
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		log.Printf("settings: %v", err)
	}
	a.cfg = cfg

	if err := mygo.App.SetOpenAtLogin(cfg.OpenAtLogin); err != nil {
		log.Printf("set open at login: %v", err)
	}
	// A tray app: the last window closing hides to the tray, it does not
	// quit. Without a listener mygo quits; with one it keeps running.
	mygo.App.OnWindowAllClosed(func() {})
	// Mark the quit so the bubbles' OnClose handlers let it close them
	// (OnBeforeQuit runs before the quit sequence closes the windows).
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.mu.Lock()
		a.quitting = true
		a.mu.Unlock()
	})
	a.bubbles = newBubbleManager(a.cancelRun, a.isQuitting)
	if err := a.applyHotkeys(cfg.Hotkeys); err != nil {
		a.notify("Sotto", err.Error())
	}
	a.applyDoubleClick(cfg.Selection)
	a.setUpTray()
	a.mu.Lock()
	log.Printf("ready: hotkeys in force: %v", a.hotkeys)
	a.mu.Unlock()
}

// settings returns a copy of the current settings.
func (a *application) settings() config.Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// updateSettings normalizes, saves and applies s.
func (a *application) updateSettings(s config.Settings) error {
	s = config.Normalize(s)
	if err := config.Save(a.cfgPath, s); err != nil {
		return err
	}
	a.applySettings(s)
	return nil
}

// applySettings swaps the settings in and follows them: autostart,
// hotkeys, the bubble's focus hiding, the tray's checkbox and every
// open page.
func (a *application) applySettings(s config.Settings) {
	a.mu.Lock()
	old := a.cfg
	a.cfg = s
	a.mu.Unlock()

	if s.OpenAtLogin != old.OpenAtLogin {
		if err := mygo.App.SetOpenAtLogin(s.OpenAtLogin); err != nil {
			log.Printf("set open at login: %v", err)
		}
	}
	if s.Hotkeys != old.Hotkeys {
		if err := a.applyHotkeys(s.Hotkeys); err != nil {
			a.notify("Sotto", err.Error())
		}
	}
	if s.Bubble.AutoHide != old.Bubble.AutoHide {
		a.bubbles.setAutoHide(s.Bubble.AutoHide)
	}
	if s.Selection != old.Selection {
		a.applyDoubleClick(s.Selection)
	}
	if a.tray != nil {
		a.tray.SetMenu(a.trayMenu())
	}
	// Hotkey fallbacks may have changed a.cfg after s was applied; send
	// what is actually in force.
	a.broadcastSettings(a.settings())
}

// broadcastSettings tells every open page the new settings.
func (a *application) broadcastSettings(s config.Settings) {
	for _, w := range a.bubbles.windows() {
		SettingsChanged.Emit(w, s)
	}
	a.mu.Lock()
	settingsWin := a.settingsWin
	a.mu.Unlock()
	if settingsWin != nil {
		SettingsChanged.Emit(settingsWin, s)
	}
}

// onHotkey is what a registered accelerator runs, on the main thread.
// The actions continue on goroutines, where slow work — a selection
// grab — and the window marshalling that follows it are safe: ShowAt
// serializes its callers, and the main thread never calls in.
func (a *application) onHotkey(action string) func() {
	return func() {
		log.Printf("hotkey: %s", action)
		switch action {
		case "panel":
			go a.showBubble("input", "")
		case "selection":
			go a.grabSelection()
		case "ocr":
			a.snip.Show()
		}
	}
}

// showBubble brings up a bubble for a query.
func (a *application) showBubble(trigger, text string) {
	a.bubbles.Show(a.settings(), trigger, text)
}

// grabSelection translates the text selected in the foreground app; the
// hotkey's path. With nothing selected it falls back to the input panel,
// which still takes the hotkey somewhere useful.
func (a *application) grabSelection() {
	sel, ok := a.grabSelectionOnce()
	if !ok {
		return // our own window is in front: nothing to grab
	}
	if sel == nil {
		a.showBubble("input", "")
		return
	}
	a.showSelectionBubble(*sel)
}

// grabSelectionAuto is the double-click watch's path: a word was just
// double clicked in some app. The hook sees the press before the app
// builds its selection, so the grab runs at checkpoints counted from the
// click itself — not after the previous attempt ends, which a slow
// attempt would push out of the user's attention — and a miss retries
// while the selection appears. There is no fallback panel: a double
// click that selects nothing — on a button, a folder icon — must stay
// silent.
func (a *application) grabSelectionAuto() {
	start := time.Now()
	for _, at := range []time.Duration{250 * time.Millisecond, 600 * time.Millisecond, 1000 * time.Millisecond} {
		if d := at - time.Since(start); d > 0 {
			time.Sleep(d)
		}
		sel, ok := a.grabSelectionOnce()
		if !ok || sel != nil {
			return
		}
	}
}

// onDoubleClick is the watch's callback, on a goroutine of its own.
func (a *application) onDoubleClick() {
	if !a.autoGrab.CompareAndSwap(false, true) {
		return // a grab is under way; its bubble will cover this click too
	}
	defer a.autoGrab.Store(false)
	a.grabSelectionAuto()
}

// grabSelectionOnce runs one grab and cleans the text the way OCR text
// is cleaned — a selection can carry zero-width or control characters
// from the source app. nil with ok means nothing was selected and a
// retry would not help; !ok means the grab must not run here at all,
// our own window being in front.
func (a *application) grabSelectionOnce() (*selection.Selection, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sel, err := selection.Grab(ctx)
	switch {
	case errors.Is(err, selection.ErrSelfWindow):
		return nil, false
	case errors.Is(err, selection.ErrNoSelection):
		return nil, true
	case err != nil:
		log.Printf("selection: %v", err)
		return nil, true
	}
	if strings.TrimSpace(ocr.CleanText(sel.Text)) == "" {
		return nil, true
	}
	return &sel, true
}

// showSelectionBubble opens the bubble for a grabbed selection — at the
// selection's corner, when the grab could see it — and runs its translation.
func (a *application) showSelectionBubble(sel selection.Selection) {
	var anchor *Anchor
	if sel.HasRect {
		anchor = &Anchor{X: sel.Rect.X + sel.Rect.W, Y: sel.Rect.Y + sel.Rect.H}
	}
	shown := a.bubbles.ShowAt(a.settings(), "selection", sel.Text, anchor)
	a.runTranslation(shown.Win, RunReq{
		ReqID:  shown.ReqID,
		Source: "selection",
		Text:   sel.Text,
	})
}

// applyDoubleClick enables or disables double-click detection on the mouse hook.
func (a *application) applyDoubleClick(sel config.SelectionSettings) {
	if sel.DoubleClick {
		if err := selection.StartDoubleClick(a.onDoubleClick); err != nil {
			log.Printf("double-click watch: %v", err)
		}
	} else {
		selection.StopDoubleClick()
	}
}

// applyHotkeys switches to the configured shortcuts, falling back when
// one is taken, and rolls back to the previous set when an action
// cannot be registered at all. It holds no lock across the calls that
// wait for the main thread.
func (a *application) applyHotkeys(cfg config.HotkeySettings) error {
	a.mu.Lock()
	old := make(map[string]string, len(a.hotkeys))
	for action, accel := range a.hotkeys {
		old[action] = accel
	}
	a.mu.Unlock()

	mygo.GlobalShortcut.UnregisterAll()
	active := map[string]string{}
	var firstErr error
	for _, action := range hotkeyActions {
		candidates := hotkeyCandidates(action, cfg)
		registered := ""
		for _, accel := range candidates {
			if err := mygo.GlobalShortcut.Register(accel, a.onHotkey(action)); err == nil {
				registered = accel
				break
			}
		}
		if registered == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("“%s”的候选快捷键（%s）都被占用",
					hotkeyLabels[action], strings.Join(candidates, "、"))
			}
			continue
		}
		if registered != candidates[0] {
			a.notify("Sotto", hotkeyLabels[action]+"的快捷键被占用，已改用 "+registered)
		}
		active[action] = registered
	}
	if firstErr != nil {
		// Put back what worked before this attempt.
		mygo.GlobalShortcut.UnregisterAll()
		for action, accel := range old {
			if err := mygo.GlobalShortcut.Register(accel, a.onHotkey(action)); err != nil {
				log.Printf("rollback hotkey %s: %v", accel, err)
			}
		}
		a.mu.Lock()
		a.hotkeys = old
		a.mu.Unlock()
		a.syncHotkeys(old)
		return firstErr
	}
	a.mu.Lock()
	a.hotkeys = active
	a.mu.Unlock()
	a.syncHotkeys(active)
	return nil
}

// syncHotkeys writes the accelerators actually in force back into the
// settings and broadcasts them. Without this the settings page keeps
// showing the combination the user typed even when registration fell back
// to another accelerator (or rolled back).
func (a *application) syncHotkeys(active map[string]string) {
	a.mu.Lock()
	next := a.cfg.Hotkeys
	if v := active["panel"]; v != "" {
		next.Panel = v
	}
	if v := active["ocr"]; v != "" {
		next.Ocr = v
	}
	if v := active["selection"]; v != "" {
		next.Selection = v
	}
	if next == a.cfg.Hotkeys {
		a.mu.Unlock()
		return
	}
	a.cfg.Hotkeys = next
	cfg := a.cfg
	path := a.cfgPath
	a.mu.Unlock()

	if path != "" {
		if err := config.Save(path, cfg); err != nil {
			log.Printf("save hotkeys: %v", err)
		}
	}
	a.broadcastSettings(cfg)
}

// hotkeyCandidates lists an action's configured accelerator followed by
// its fallbacks, without repeats.
func hotkeyCandidates(action string, cfg config.HotkeySettings) []string {
	configured := map[string]string{
		"panel":     cfg.Panel,
		"selection": cfg.Selection,
		"ocr":       cfg.Ocr,
	}[action]
	var out []string
	for _, accel := range append([]string{configured}, hotkeyFallbacks[action]...) {
		if accel != "" && !containsString(out, accel) {
			out = append(out, accel)
		}
	}
	return out
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (a *application) setUpTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: iconPNG, ToolTip: "Sotto 翻译"})
	if err != nil {
		log.Printf("no tray icon: %v", err)
		return
	}
	a.tray = tray
	tray.SetMenu(a.trayMenu())
	tray.OnClick(func() { go a.showBubble("input", "") })
}

// trayMenu is rebuilt whenever the settings change, so its checkbox
// follows autostart.
func (a *application) trayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "打开翻译面板", Click: func(*mygo.MenuItem, *mygo.Window) {
			go a.showBubble("input", "")
		}},
		{Label: "设置", Click: func(*mygo.MenuItem, *mygo.Window) { a.showSettings() }},
		mygo.Separator(),
		{Label: "开机自启", Type: mygo.MenuItemCheckbox, Checked: mygo.App.OpenAtLogin(),
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				cfg := a.settings()
				cfg.OpenAtLogin = it.Checked
				if err := a.updateSettings(cfg); err != nil {
					log.Printf("save settings: %v", err)
				}
			}},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
}

// showSettings opens the settings window, or focuses the one there is.
// It runs on the main thread (the tray); NewWindow executes directly
// there, outside the mutex.
func (a *application) showSettings() {
	a.mu.Lock()
	win := a.settingsWin
	a.mu.Unlock()
	if win != nil {
		win.Show()
		win.Focus()
		return
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:           "Sotto 设置",
		URL:             "/#/settings",
		Width:           780,
		Height:          600,
		MinWidth:        660,
		MinHeight:       500,
		StateKey:        "settings",
		BackgroundColor: "light-dark(#f6f7f9, #0f1115)",
		Page:            mygo.PageOptions{DevTools: mygo.DevToolsAuto},
	})
	a.mu.Lock()
	a.settingsWin = w
	a.mu.Unlock()
	w.OnClosed(func() {
		a.mu.Lock()
		a.settingsWin = nil
		a.mu.Unlock()
	})
	w.Show()
}

// RunReq is what a page sends to start a translation.
type RunReq struct {
	ReqID      string `json:"reqID"`
	Source     string `json:"source"` // "input" | "selection" | "ocr"
	Text       string `json:"text"`
	SourceLang string `json:"sourceLang"` // "" or "auto" detects
	TargetLang string `json:"targetLang"` // "" uses the default
}

// runTranslation resolves the languages, fans the request out to the
// services and pushes every result to win.
func (a *application) runTranslation(win *mygo.Window, req RunReq) {
	if req.ReqID == "" {
		req.ReqID = newRequestID()
	}
	cfg := a.settings()

	text := req.Text
	// Identifiers translate better as words: getUserInfo goes to the
	// services as "get user info".
	if cfg.Identifier.AutoSplit && naming.DetectStyle(text) != naming.None &&
		langdetect.Detect(text) == "latin" {
		text = strings.Join(naming.SplitWords(text), " ")
	}

	// A lone English word also gets a dictionary card: the phonetics
	// and the main senses, beside the translations.
	singleWord := isSingleWord(text)

	detected := langdetect.Detect(text)
	source := req.SourceLang
	if source == "" {
		source = "auto"
	}
	target := req.TargetLang
	if target == "" {
		target = cfg.TargetLanguage
	}
	if target == "auto" || sameLanguage(detected, target) {
		target = defaultTarget(detected)
	}
	// The services that will actually answer, so the page does not keep
	// skeleton cards for the ones without credentials.
	RunMeta.Emit(win, RunMetaMsg{
		ReqID:      req.ReqID,
		SourceLang: source,
		TargetLang: target,
		Services:   translate.Available(cfg, a.keys),
	})

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.runs[req.ReqID] = cancel
	a.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			a.mu.Lock()
			delete(a.runs, req.ReqID)
			a.mu.Unlock()
		}()
		translate.Run(ctx, cfg, a.keys, translate.Request{
			Text:       text,
			SourceLang: source,
			TargetLang: target,
			SingleWord: singleWord,
		}, func(res translate.Result) {
			TranslationUpdate.Emit(win, TranslationUpdateMsg{
				ReqID:        req.ReqID,
				Service:      res.Service,
				Text:         res.Text,
				DetectedLang: res.DetectedLang,
				Err:          res.Err,
			})
		})
	}()
}

// isSingleWord reports whether text is one bare English word — letters
// only, no spaces, punctuation or digits — which the dictionary covers.
func isSingleWord(text string) bool {
	if text == "" || len(text) > 45 {
		return false
	}
	for _, r := range text {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return langdetect.Detect(text) == "latin"
}

// cancelRun stops a running translation, when its bubble is reused.
func (a *application) cancelRun(reqID string) {
	a.mu.Lock()
	cancel := a.runs[reqID]
	delete(a.runs, reqID)
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// defaultTarget flips between Chinese and English: anything but Chinese
// translates into Chinese, Chinese into English.
func defaultTarget(detected string) string {
	if detected == "zh" {
		return "en"
	}
	return "zh-HANS"
}

// sameLanguage reports whether a langdetect code and an app language
// code name the same language.
func sameLanguage(detected, target string) bool {
	switch {
	case detected == "zh" && target == "zh-HANS":
		return true
	case detected == "latin" && target == "en":
		return true
	default:
		return detected == target
	}
}

func (a *application) notify(title, body string) {
	if err := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show(); err != nil {
		log.Printf("notification: %v", err)
	}
}

// newRequestID identifies one translation run.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%d", b[0]) // crypto/rand failing is unheard of
	}
	return hex.EncodeToString(b[:])
}
