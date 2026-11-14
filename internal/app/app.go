package app

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/hotkey"
	"sotto/internal/keys"
	"sotto/internal/langdetect"
	"sotto/internal/naming"
	"sotto/internal/platform"
	"sotto/internal/selection"
	"sotto/internal/speech"
	"sotto/internal/translate"
	"sotto/internal/ui/bubble"
	"sotto/internal/ui/settings"
	"sotto/internal/ui/snip"
)

// application is the app's state: the settings, the credential store,
// the bubble windows and the settings window. Its mutex guards the
// fields and is never held across a call that waits for the main thread.
type application struct {
	iconPNG       []byte
	cfgPath       string
	configWriteMu sync.Mutex

	mu              sync.Mutex
	cfg             config.Settings
	hotkeys         map[string]string // action → the accelerator in force
	hotkeyErrors    map[string]string
	shortcutBackend hotkey.Backend
	entry           entryGate
	settingsWin     *mygo.Window
	settingsView    *settings.View
	quitting        bool

	bubbles *bubble.Manager
	snip    *snip.Manager
	tray    *mygo.Tray // main thread only
	keys    keys.Store
}

func newApplication(iconPNG []byte) *application {
	return &application{iconPNG: iconPNG, keys: keys.Store{}, snip: &snip.Manager{}}
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
	cfg.Hotkeys = canonicalHotkeys(cfg.Hotkeys)
	a.cfg = cfg

	// Restore checks without overwriting choices made in the update window.
	platform.SetAutomaticUpdates(cfg.AutoUpdate)
	if err := mygo.App.SetOpenAtLogin(cfg.OpenAtLogin); err != nil {
		log.Printf("set open at login: %v", err)
	}
	// A tray app: the last window closing hides to the tray, it does not
	// quit. Without a listener mygo quits; with one it keeps running.
	mygo.App.OnWindowAllClosed(func() {})
	// Mark the quit so the bubbles' OnClose handlers let it close them
	// (OnBeforeQuit runs before the quit sequence closes the windows).
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		a.mu.Lock()
		a.quitting = true
		a.mu.Unlock()
		a.entry.cancel()
		a.mu.Lock()
		sv := a.settingsView
		a.mu.Unlock()
		if sv != nil && !sv.PrepareQuit(mygo.App.Quit) {
			e.PreventDefault()
		}
	})
	speaker := speech.New()
	mygo.App.OnQuit(speaker.Close)
	a.bubbles = bubble.NewManager(a.isQuitting, bubble.Actions{
		Speak:        speaker.Speak,
		Run:          a.runTranslation,
		Settings:     a.settings,
		StartOCR:     func() { a.startEntry("ocr") },
		OpenSettings: func() { a.showSettings(true) },
	})
	mygo.Screen.OnDisplaysChanged(a.bubbles.RefreshDisplays)
	a.snip.IsQuitting = a.isQuitting
	a.snip.Done = a.snipDone
	mygo.Screen.OnDisplaysChanged(a.snip.RefreshDisplays)
	a.readyPlatform()
	a.shortcutBackend = hotkey.NewBackend()
	mygo.App.OnQuit(a.shortcutBackend.Close)
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
	return a.saveSettingsWithHotkeys(s, "")
}

// applySettings swaps the settings in and follows them: autostart,
// update checks, hotkeys, the bubble's focus hiding, the tray's checkbox and every
// open page.
func (a *application) applySettings(s config.Settings) {
	a.mu.Lock()
	old := a.cfg
	a.cfg = s
	a.mu.Unlock()

	if s.AutoUpdate != old.AutoUpdate {
		platform.SetAutomaticUpdates(s.AutoUpdate)
	}
	if s.OpenAtLogin != old.OpenAtLogin {
		if err := mygo.App.SetOpenAtLogin(s.OpenAtLogin); err != nil {
			log.Printf("set open at login: %v", err)
		}
		a.refreshTrayMenu()
	}
	if s.Bubble.AutoHide != old.Bubble.AutoHide {
		a.bubbles.SetAutoHide(s.Bubble.AutoHide)
	}
	// Publish only after registration and persistence have succeeded.
	a.broadcastSettings(a.settings())
}

// broadcastSettings tells open native bubbles and the settings window the new settings.
func (a *application) broadcastSettings(s config.Settings) {
	a.bubbles.ApplySettings(s)
	a.mu.Lock()
	sv := a.settingsView
	a.mu.Unlock()
	if sv != nil {
		sv.ApplyExternalSettings(s)
	}
}

// startEntry runs on the UI thread: capture the source before dispatching.
func (a *application) startEntry(action string) {
	if a.isQuitting() {
		return
	}
	// Preserve the active input page before begin revokes its entry ownership.
	if action == "panel" && a.bubbles.FocusVisibleInput() {
		return
	}
	task := a.entry.begin()
	log.Printf("entry: start id=%d action=%s", task.ID(), action)
	switch action {
	case "panel":
		a.snip.Hide()
		a.bubbles.HideTransient()
		go a.bubbles.ShowTask(a.settings(), "input", "", nil, "", "", task.Valid, nil)
	case "selection":
		grab := selection.Prepare()
		a.snip.Hide()
		a.bubbles.HideTransient()
		go a.grabSelection(task, grab)
	case "ocr":
		a.bubbles.HideTransient()
		a.startScreenshot(task)
	}
}

func (a *application) grabSelection(task *entryTask, grab func(context.Context) (selection.Selection, error)) {
	ctx, cancel := context.WithTimeout(task.Context(), 5*time.Second)
	defer cancel()
	started := time.Now()
	sel, err := grab(ctx)
	valid := task.Valid()
	log.Printf("selection: end entry=%d current=%t via=%s elapsed=%s err=%v", task.ID(), valid, sel.Via, time.Since(started).Round(time.Millisecond), err)
	if !valid || errors.Is(err, selection.ErrSelfWindow) || errors.Is(err, selection.ErrSourceChanged) {
		return
	}
	message := ""
	switch {
	case platform.SelectionPermissionMessage(err) != "":
		message = platform.SelectionPermissionMessage(err)
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
	var anchor *bubble.Anchor
	if sel.HasRect {
		anchor = &bubble.Anchor{X: sel.Rect.X + sel.Rect.W, Y: sel.Rect.Y + sel.Rect.H}
	}
	phase := ""
	if message != "" {
		phase = "error"
	}
	shown := a.bubbles.ShowTask(a.settings(), "selection", sel.Text, anchor, phase, message, task.Valid, nil)
	if shown.Win != nil && message == "" && task.Valid() {
		a.runTranslation(shown.Win, bubble.RunReq{ReqID: shown.ReqID, SessionID: shown.ReqID, Revision: 0, Source: "selection", Text: sel.Text})
	}
}

func (a *application) setUpTray() {
	tray, err := mygo.NewTray(platform.TrayOptions(mygo.TrayOptions{
		Icon:    a.iconPNG,
		ToolTip: "Sotto 翻译",
		Menu:    a.trayMenu(),
	}))
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

	items := []*mygo.MenuItem{
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
		platform.UpdateMenuItem(a.refreshTrayMenu),
		{Label: "开机自启", Type: mygo.MenuItemCheckbox, Checked: mygo.App.OpenAtLogin(),
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				go a.setTrayOpenAtLogin(it.IsChecked())
			}},
	}
	items = append(items, a.platformMenuItems()...)
	items = append(items, mygo.Separator(), &mygo.MenuItem{Label: "退出 Sotto", Role: mygo.RoleQuit})
	return mygo.NewMenu(items)
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
	if a.isQuitting() {
		return
	}
	a.mu.Lock()
	win := a.settingsWin
	sv := a.settingsView
	a.mu.Unlock()
	if win != nil {
		if services && sv != nil {
			sv.SetActiveTab(settings.TabServices)
		}
		win.Show()
		win.Focus()
		return
	}
	tab := settings.TabGeneral
	if services {
		tab = settings.TabServices
	}
	svc := ConfigSvc{a}
	if sv == nil {
		sv = settings.NewView(tab, a.settings(), settings.Actions{
			GetSettings:  svc.Get,
			SaveSettings: svc.Set,
			Credentials:  func() (settings.CredentialStatus, error) { return svc.Credentials(), nil },
			SetSecret:    svc.SetSecret,
			TestService:  svc.TestService,
			OpenKeyPage:  svc.OpenKeyPage,
			DeeplUsage:   svc.DeeplUsage,
			SaveHotkey:   a.updateHotkey,
			HotkeyErrors: a.shortcutErrors,
			Invalidate: func() {
				a.mu.Lock()
				w := a.settingsWin
				a.mu.Unlock()
				if w != nil {
					w.Invalidate()
				}
			},
		})
	} else {
		sv.SetActiveTab(tab)
		sv.RefreshCredentialsAsync()
	}
	w := mygo.NewWindow(platform.SettingsWindow(mygo.WindowOptions{
		Title:           "Sotto 设置",
		Content:         ui.View(sv.Render),
		Width:           780,
		Height:          600,
		MinWidth:        660,
		MinHeight:       500,
		StateKey:        "settings",
		BackgroundColor: "light-dark(#ffffff, #222325)",
	}))
	a.mu.Lock()
	a.settingsWin = w
	a.settingsView = sv
	a.mu.Unlock()
	w.OnBlur(sv.CancelHotkeyRecording)
	w.OnClosed(func() {
		sv.DetachWindow()
		a.mu.Lock()
		if a.settingsWin == w {
			a.settingsWin = nil
		}
		a.mu.Unlock()
	})
	w.Show()
}

// runTranslation resolves the languages, fans the request out to the
// services and pushes every result to win.
func (a *application) runTranslation(win *mygo.Window, req bubble.RunReq) {
	if req.ReqID == "" {
		req.ReqID = bubble.NewRequestID()
	}
	cfg := a.settings()

	text := translate.PrepareSource(req.Text, cfg.Identifier)
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
	ctx, accepted := a.bubbles.StartRun(win, req)
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
	meta := bubble.RunMetaMsg{ReqID: req.ReqID, SourceLang: source, TargetLang: target, Services: services}
	if !a.bubbles.RecordMeta(win, meta, req.Service) {
		a.bubbles.FinishRun(win, req.ReqID)
		return
	}
	a.bubbles.PushMeta(win, meta)
	if req.Service != "" && !slices.Contains(services, req.Service) {
		msg := bubble.TranslationUpdateMsg{ReqID: req.ReqID, Service: req.Service, Err: "该服务当前不可用，请检查设置"}
		if a.bubbles.RecordResult(win, msg) {
			a.bubbles.PushResult(win, msg)
		}
		a.bubbles.FinishRun(win, req.ReqID)
		return
	}
	go func() {
		defer a.bubbles.FinishRun(win, req.ReqID)
		emit := func(res translate.Result) {
			if ctx.Err() != nil {
				return
			}
			msg := bubble.TranslationUpdateMsg{ReqID: req.ReqID, Service: res.Service, Text: res.Text, DetectedLang: res.DetectedLang, Err: res.Err, Streaming: res.Streaming}
			if a.bubbles.RecordResult(win, msg) {
				a.bubbles.PushResult(win, msg)
			}
		}
		if text == "" {
			for _, service := range cfg.Services.Enabled {
				if slices.Contains(services, service) {
					emit(translate.Result{Service: service, Err: "清理后的原文为空，请修改原文或调整命名转换设置"})
				}
			}
			return
		}
		translate.Run(ctx, cfg, credentials, translate.Request{Text: text, SourceLang: source, TargetLang: target, SingleWord: singleWord}, emit)
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
