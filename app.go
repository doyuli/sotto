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
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"

	"sotto/internal/config"
	"sotto/internal/keys"
	"sotto/internal/langdetect"
	"sotto/internal/naming"
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
	cfgPath       string
	configWriteMu sync.Mutex

	mu          sync.Mutex
	cfg         config.Settings
	hotkeys     map[string]string // action → the accelerator in force
	entry       entryGate
	settingsWin *mygo.Window
	quitting    bool

	bubbles *bubbleManager
	snip    *snipManager
	tray    *mygo.Tray // main thread only
	keys    keys.Store
}

func newApplication() *application {
	return &application{keys: keys.Store{}, snip: &snipManager{}}
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
	cfg.Services.Enabled = translate.Available(cfg, a.keys)
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
		a.entry.cancel()
	})
	a.bubbles = newBubbleManager(a.isQuitting)
	a.snip.isQuitting = a.isQuitting
	if runtime.GOOS == "windows" {
		// Start WebView2 initialization while the app is launching, before
		// the first selection needs a window. The page stays hidden until used.
		a.bubbles.current = a.bubbles.create(cfg)
	}
	if err := a.applyHotkeys(cfg.Hotkeys); err != nil {
		a.notify("Sotto", err.Error())
	}
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
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	s = config.Normalize(s)
	s.Services.Enabled = translate.Available(s, a.keys)
	return a.saveSettings(s)
}

// saveSettings commits settings while the caller holds configWriteMu.
func (a *application) saveSettings(s config.Settings) error {
	if err := config.Save(a.cfgPath, s); err != nil {
		return fmt.Errorf("保存设置：%w", err)
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
		a.refreshTrayMenu()
	}
	if s.Hotkeys != old.Hotkeys {
		if err := a.applyHotkeys(s.Hotkeys); err != nil {
			a.notify("Sotto", err.Error())
		}
	}
	if s.Bubble.AutoHide != old.Bubble.AutoHide {
		a.bubbles.setAutoHide(s.Bubble.AutoHide)
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
// grab — and the window marshalling that follows it are safe: ShowTask
// serializes worker callers before dispatching the presentation to the UI thread.
func (a *application) onHotkey(action string) func() {
	return func() { a.startEntry(action) }
}

// startEntry runs on the UI thread: capture the source before dispatching.
func (a *application) startEntry(action string) {
	task := a.entry.begin()
	log.Printf("entry: start id=%d action=%s", task.id, action)
	switch action {
	case "panel":
		a.snip.Hide()
		go a.bubbles.ShowTask(a.settings(), "input", "", nil, "", "", task.valid, nil)
	case "selection":
		grab := selection.Prepare()
		a.snip.Hide()
		go a.grabSelection(task, grab)
	case "ocr":
		a.bubbles.hideTransient()
		a.snip.Show(task)
	}
}

func (a *application) grabSelection(task *entryTask, grab func(context.Context) (selection.Selection, error)) {
	ctx, cancel := context.WithTimeout(task.ctx, 5*time.Second)
	defer cancel()
	started := time.Now()
	sel, err := grab(ctx)
	valid := task.valid()
	log.Printf("selection: end entry=%d current=%t via=%s elapsed=%s err=%v", task.id, valid, sel.Via, time.Since(started).Round(time.Millisecond), err)
	if !valid || errors.Is(err, selection.ErrSelfWindow) || errors.Is(err, selection.ErrSourceChanged) {
		return
	}
	message := ""
	switch {
	case errors.Is(err, selection.ErrClipboardRestore):
		message = "剪贴板未完整恢复，请先处理之前复制的内容。"
	case errors.Is(err, selection.ErrClipboardUnsafe):
		message = "当前剪贴板内容无法安全保留，请使用截图翻译或直接输入。"
	case errors.Is(err, selection.ErrShortcutHeld):
		message = "请松开快捷键和鼠标按键后重试，或直接输入要翻译的文字。"
	case errors.Is(err, selection.ErrNoSelection):
		message = "未读取到选中文字，请重新选中后重试，或直接输入。"
	case errors.Is(err, context.DeadlineExceeded):
		message = "读取选中文字超时，请重试或使用截图翻译。"
	case err != nil:
		message = "当前应用暂时无法取词，请使用截图翻译或直接输入。"
	}
	if err != nil {
		log.Printf("selection: %v", err)
	}
	// Selection text is not OCR: preserve tabs, paragraph spacing and joiners.
	sel.Text = strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(sel.Text, "\ufeff"), "\r\n", "\n"))
	if sel.Text == "" && message == "" {
		message = "未读取到选中文字，请重新选中后重试，或直接输入。"
	}
	var anchor *Anchor
	if sel.HasRect {
		anchor = &Anchor{X: sel.Rect.X + sel.Rect.W, Y: sel.Rect.Y + sel.Rect.H}
	}
	phase := ""
	if message != "" {
		phase = "error"
	}
	shown := a.bubbles.ShowTask(a.settings(), "selection", sel.Text, anchor, phase, message, task.valid, nil)
	if shown.Win != nil && message == "" && task.valid() {
		a.runTranslation(shown.Win, RunReq{ReqID: shown.ReqID, SessionID: shown.ReqID, Revision: 0, Source: "selection", Text: sel.Text})
	}
}

// applyHotkeys switches to the configured shortcuts, falling back when
// one is taken, and rolls back to the previous set when an action
// cannot be registered at all. It holds no lock across the calls that
// wait for the main thread.
func (a *application) applyHotkeys(cfg config.HotkeySettings) error {
	defer a.refreshTrayMenu()
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
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    iconPNG,
		ToolTip: "Sotto 翻译",
		Menu:    a.trayMenu(),
	})
	if err != nil {
		log.Printf("no tray icon: %v", err)
		return
	}
	a.tray = tray
	tray.OnClick(func() { a.startEntry("panel") })
}

// refreshTrayMenu publishes the autostart and hotkey states actually in force.
func (a *application) refreshTrayMenu() {
	mygo.RunOnMain(func() {
		if a.tray != nil {
			a.tray.SetMenu(a.trayMenu())
		}
	})
}

func (a *application) trayMenu() *mygo.Menu {
	label := "Sotto"
	if version := mygo.App.Version(); version != "" {
		label += " v" + version
	}
	a.mu.Lock()
	panelKey, ocrKey := a.hotkeys["panel"], a.hotkeys["ocr"]
	a.mu.Unlock()

	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: label, Disabled: true},
		mygo.Separator(),
		{Label: "翻译面板", Accelerator: panelKey, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.startEntry("panel")
		}},
		{Label: "截图翻译", Accelerator: ocrKey, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.startEntry("ocr")
		}},
		mygo.Separator(),
		{Label: "设置", Click: func(*mygo.MenuItem, *mygo.Window) { a.showSettings(false) }},
		updater.MenuItem(),
		{Label: "开机自启", Type: mygo.MenuItemCheckbox, Checked: mygo.App.OpenAtLogin(),
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				go a.setTrayOpenAtLogin(it.IsChecked())
			}},
		mygo.Separator(),
		{Label: "退出 Sotto", Role: mygo.RoleQuit},
	})
}

func (a *application) setTrayOpenAtLogin(open bool) {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	cfg := a.settings()
	if cfg.OpenAtLogin == open {
		// Windows can change the startup entry independently of our settings.
		if err := mygo.App.SetOpenAtLogin(open); err != nil {
			log.Printf("set open at login: %v", err)
		}
		a.refreshTrayMenu()
		return
	}
	cfg.OpenAtLogin = open
	if err := a.saveSettings(cfg); err != nil {
		log.Printf("save settings: %v", err)
		a.refreshTrayMenu()
	}
}

// showSettings opens the settings window, or focuses the one there is.
// It runs on the main thread (the tray); NewWindow executes directly
// there, outside the mutex.
func (a *application) showSettings(services bool) {
	a.mu.Lock()
	win := a.settingsWin
	a.mu.Unlock()
	if win != nil {
		if services {
			OpenTranslationSettings.Emit(win, true)
		}
		win.Show()
		win.Focus()
		return
	}
	url := "/settings.html"
	if services {
		url += "?tab=services"
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:           "Sotto 设置",
		URL:             url,
		Width:           780,
		Height:          600,
		MinWidth:        660,
		MinHeight:       500,
		StateKey:        "settings",
		BackgroundColor: "light-dark(#ffffff, #222325)",
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
	SessionID  string `json:"sessionID"`
	Revision   int64  `json:"revision"`
	Service    string `json:"service"`
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
	target := resolveTarget(detected, req.TargetLang, cfg.TargetLanguage)
	// Resolve the actual request before publishing metadata or starting I/O.
	req.SourceLang, req.TargetLang = source, target
	ctx, accepted := a.bubbles.startRun(win, req)
	if !accepted {
		return
	}
	credentials := a.keys.Snapshot()
	services := translate.Available(cfg, credentials)
	if req.Service != "" {
		cfg.Services.Enabled = nil
		if slices.Contains(services, req.Service) {
			cfg.Services.Enabled = []string{req.Service}
		}
	}
	meta := RunMetaMsg{ReqID: req.ReqID, SourceLang: source, TargetLang: target, Services: services}
	if a.bubbles.recordMeta(win, meta, req.Service) {
		RunMeta.Emit(win, meta)
	}
	if req.Service != "" && !slices.Contains(services, req.Service) {
		msg := TranslationUpdateMsg{ReqID: req.ReqID, Service: req.Service, Err: "该服务当前不可用，请检查设置"}
		if a.bubbles.recordResult(win, msg) {
			TranslationUpdate.Emit(win, msg)
		}
		a.bubbles.finishRun(win, req.ReqID)
		return
	}
	go func() {
		defer a.bubbles.finishRun(win, req.ReqID)
		translate.Run(ctx, cfg, credentials, translate.Request{Text: text, SourceLang: source, TargetLang: target, SingleWord: singleWord}, func(res translate.Result) {
			if ctx.Err() != nil {
				return
			}
			msg := TranslationUpdateMsg{ReqID: req.ReqID, Service: res.Service, Text: res.Text, DetectedLang: res.DetectedLang, Err: res.Err}
			if a.bubbles.recordResult(win, msg) {
				TranslationUpdate.Emit(win, msg)
			}
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

// Explicit targets are never rewritten by script-level detection.
func resolveTarget(detected, requested, configured string) string {
	if requested != "" && requested != "auto" {
		return requested
	}
	target := configured
	if requested == "auto" {
		target = "auto"
	}
	if target == "auto" || sameLanguage(detected, target) {
		return defaultTarget(detected)
	}
	return target
}
