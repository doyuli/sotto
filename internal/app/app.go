package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

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
	"sotto/internal/ui/overlay"
	"sotto/internal/ui/settings"
	"sotto/internal/ui/snip"
	"sotto/internal/usermessage"
)

// application is the app's state: the settings, the credential store,
// the bubble windows and the settings window. Its mutex guards the
// fields and is never held across a call that waits for the main thread.
type application struct {
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

	bubbles     *bubble.Manager
	overlays    overlay.Manager
	minimalJobs map[int64]*minimalJob // main thread only
	snip        *snip.Manager
	tray        *mygo.Tray // main thread only
	keys        credentialStore
}

func newApplication() *application {
	return &application{keys: keys.Store{}, snip: &snip.Manager{}}
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
		a.stopMinimalJobs()
		a.snip.HideStale()
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
	mygo.Screen.OnDisplaysChanged(a.overlays.RefreshDisplays)
	a.snip.IsQuitting = a.isQuitting
	a.snip.Done = a.snipDone
	a.snip.Failed = a.snipFailed
	a.snip.SuspendResults = a.overlays.Suspend
	a.shortcutBackend = hotkey.NewBackend()
	a.snip.RegisterEscape = a.shortcutBackend.RegisterEscape
	mygo.Screen.OnDisplaysChanged(a.snip.RefreshDisplays)
	a.readyPlatform()
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

// A queued form snapshot may predate a shortcut or tray change. Those two
// preferences are committed independently against the latest settings.
func (a *application) updateSettings(s config.Settings) error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	current := a.settings()
	s.MinimalTranslation = current.MinimalTranslation
	s.OpenAtLogin = current.OpenAtLogin
	return a.saveSettings(config.Normalize(s))
}

func (a *application) setMinimalTranslation(enabled bool) error {
	_, err := a.updateMinimalTranslation(func(bool) bool { return enabled })
	return err
}

func (a *application) updateMinimalTranslation(update func(bool) bool) (bool, error) {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	cfg := a.settings()
	previous := cfg.MinimalTranslation
	cfg.MinimalTranslation = update(previous)
	if cfg.MinimalTranslation == previous {
		return previous, nil
	}
	if err := a.saveSettings(cfg); err != nil {
		return previous, err
	}
	return cfg.MinimalTranslation, nil
}

// saveSettings commits settings while the caller holds configWriteMu.
func (a *application) saveSettings(s config.Settings) error {
	return a.saveSettingsWithHotkeys(s, "")
}

// applySettings publishes committed preferences to the updater and open views.
func (a *application) applySettings(s config.Settings) {
	a.mu.Lock()
	old := a.cfg
	a.cfg = s
	a.mu.Unlock()

	if s.AutoUpdate != old.AutoUpdate {
		platform.SetAutomaticUpdates(s.AutoUpdate)
	}
	if s.Bubble.AutoHide != old.Bubble.AutoHide {
		a.bubbles.SetAutoHide(s.Bubble.AutoHide)
	}
	if s.MinimalTranslation != old.MinimalTranslation && mygo.App.IsReady() {
		mygo.RunOnMain(a.refreshTrayMode)
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
	// A mode switch applies to the next entry, even while this one is acquiring input.
	task := a.entry.begin(action != "panel" && a.settings().MinimalTranslation)
	if action == "selection" || task.minimal {
		task.cursor = mygo.Screen.CursorScreenPoint()
	}
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
		a.snip.HideStale()
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
	if message == "" {
		if err := translate.ValidateInput(sel.Text); err != nil {
			sel.Text = ""
			message = translate.UserMessage(err)
		}
	}
	source := task.selectionSource(sel)
	if task.minimal && (message == "" || selection.PendingClipboardRecovery() == "") {
		if message != "" {
			a.showMinimalEntryError(task, source, message)
		} else {
			mygo.RunOnMain(func() {
				a.startMinimalJob(task, source, func(ctx context.Context, progress func(string)) translate.Result {
					return a.minimalTranslation(ctx, sel.Text, progress)
				})
			})
		}
		return
	}
	phase := ""
	if message != "" {
		phase = "error"
	}
	anchor := &bubble.Anchor{X: source.X + source.Width, Y: source.Y + source.Height}
	shown := a.bubbles.ShowTask(a.settings(), "selection", sel.Text, anchor, phase, message, task.Valid, nil)
	if shown.Win != nil && message == "" && task.Valid() {
		a.runTranslation(shown.Win, bubble.RunReq{ReqID: shown.ReqID, SessionID: shown.ReqID, Revision: 0, Source: "selection", Text: sel.Text})
	}
}

func (a *application) trayOptions() mygo.TrayOptions {
	return platform.TrayOptions(a.settings().MinimalTranslation)
}

func (a *application) setUpTray() {
	opts := a.trayOptions()
	opts.Menu = a.trayMenu()
	tray, err := mygo.NewTray(opts)
	if err != nil {
		log.Printf("no tray icon: %v", err)
		return
	}
	a.tray = tray
	tray.OnClick(func() { a.startEntry("panel") })
}

func (a *application) refreshTrayMode() {
	if a.tray == nil {
		return
	}
	opts := a.trayOptions()
	if err := a.tray.SetIcon(opts.Icon, opts.IconIsTemplate); err != nil {
		log.Printf("update tray icon: %v", err)
	}
	a.tray.SetToolTip(opts.ToolTip)
	a.refreshTrayMenu()
}

// refreshTrayMenu publishes current mode, shortcut, update and window state.
func (a *application) refreshTrayMenu() {
	mygo.RunOnMain(func() {
		if a.tray != nil {
			a.tray.SetMenu(a.trayMenu())
		}
	})
}

func (a *application) trayMenu() *mygo.Menu {
	items := append(a.trayTranslationItems(), mygo.Separator(), &mygo.MenuItem{
		Label: "设置", Click: func(*mygo.MenuItem, *mygo.Window) { a.showSettings(false) },
	})
	items = append(items, a.platformMenuItems()...)
	items = append(items, platform.UpdateMenuItem(a.refreshTrayMenu), mygo.Separator(), &mygo.MenuItem{Label: "退出 Sotto", Role: mygo.RoleQuit})
	return mygo.NewMenu(items)
}

func (a *application) trayTranslationItems() []*mygo.MenuItem {
	a.mu.Lock()
	panelKey, ocrKey, selectionKey := a.hotkeys["panel"], a.hotkeys["ocr"], a.hotkeys["selection"]
	modeKey, minimal := a.hotkeys["minimalTranslation"], a.cfg.MinimalTranslation
	a.mu.Unlock()

	// Hint at the shortcut without taking focus from the source application.
	selectionHint := &mygo.MenuItem{ID: "selection", Label: hotkeyLabels["selection"], Accelerator: selectionKey, Disabled: true}
	if selectionKey == "" {
		selectionHint.Label += "（快捷键未生效）"
	}

	return []*mygo.MenuItem{
		{Label: hotkeyLabels["panel"], Accelerator: panelKey, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.startEntry("panel")
		}},
		{Label: hotkeyLabels["ocr"], Accelerator: ocrKey, Click: func(*mygo.MenuItem, *mygo.Window) {
			a.startEntry("ocr")
		}},
		selectionHint,
		mygo.Separator(),
		{ID: "minimalTranslation", Label: hotkeyLabels["minimalTranslation"], Accelerator: modeKey,
			Type: mygo.MenuItemCheckbox, Checked: minimal, Click: func(*mygo.MenuItem, *mygo.Window) {
				go a.toggleMinimalTranslation()
			}},
	}
}

func (a *application) setOpenAtLogin(open bool) error {
	return a.commitOpenAtLogin(open, mygo.App.SetOpenAtLogin)
}

func (a *application) commitOpenAtLogin(open bool, setLoginItem func(bool) error) error {
	a.configWriteMu.Lock()
	defer a.configWriteMu.Unlock()

	cfg := a.settings()
	if cfg.OpenAtLogin != open {
		cfg.OpenAtLogin = open
		if err := a.saveSettings(cfg); err != nil {
			return err
		}
	}
	// Windows can change the startup entry independently of our settings.
	if err := setLoginItem(open); err != nil {
		return usermessage.Explain(fmt.Errorf("set open at login: %w", err), "请在系统设置中检查 Sotto 的启动项")
	}
	return nil
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
		if sv != nil {
			if services {
				sv.SetActiveTab(settings.TabServices)
			}
			sv.RefreshOpenAtLogin()
		}
		win.Show()
		win.Focus()
		return
	}
	tab := settings.TabGeneral
	if services {
		tab = settings.TabServices
	}
	if sv == nil {
		sv = a.createSettingsView(tab)
	} else {
		sv.SetActiveTab(tab)
		sv.RefreshCredentialsAsync()
	}
	sv.RefreshOpenAtLogin()
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
	a.mu.Unlock()
	w.OnFocus(sv.RefreshOpenAtLogin)
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

func (a *application) createSettingsView(tab settings.Tab) *settings.View {
	svc := ConfigSvc{a}
	actions := settings.Actions{
		GetSettings:            svc.Get,
		SaveSettings:           svc.Set,
		SaveMinimalTranslation: a.setMinimalTranslation,
		ReadOpenAtLogin:        mygo.App.OpenAtLogin,
		SaveOpenAtLogin:        a.setOpenAtLogin,
		Credentials:            svc.Credentials,
		SaveService:            svc.SaveService,
		TestService:            svc.TestService,
		OpenKeyPage:            svc.OpenKeyPage,
		DeeplUsage:             svc.DeeplUsage,
		SaveHotkey:             a.updateHotkey,
		HotkeyErrors:           a.shortcutErrors,
		Invalidate: func() {
			a.mu.Lock()
			w := a.settingsWin
			a.mu.Unlock()
			if w != nil {
				w.Invalidate()
			}
		},
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Seed and register the view together so a toggle during native window
	// creation can publish to it.
	view := settings.NewView(tab, a.cfg, actions)
	a.settingsView = view
	return view
}

// runTranslation resolves the languages, fans the request out to the
// services and pushes every result to win.
func (a *application) runTranslation(win *mygo.Window, req bubble.RunReq) {
	if req.ReqID == "" {
		req.ReqID = bubble.NewRequestID()
	}
	cfg := a.settings()

	request, inputErr := prepareTranslationRequest(req.Text, req.SourceLang, req.TargetLang, cfg)
	if inputErr == nil {
		// Resolve the actual request before publishing metadata or starting I/O.
		req.SourceLang, req.TargetLang = request.SourceLang, request.TargetLang
	}
	ctx, accepted := a.bubbles.StartRun(win, req)
	if !accepted {
		return
	}
	if inputErr != nil {
		defer a.bubbles.FinishRun(win, req.ReqID)
		message := translate.UserMessage(inputErr)
		if req.Service == "" {
			a.bubbles.PushMeta(win, bubble.RunMetaMsg{ReqID: req.ReqID, InputError: message}, "")
		} else {
			a.bubbles.PushResult(win, bubble.TranslationUpdateMsg{ReqID: req.ReqID, Service: req.Service, Err: message})
		}
		return
	}
	credentials, err := a.translationCredentials(ctx, &cfg.Services, req.Service)
	if err != nil {
		a.bubbles.FinishRun(win, req.ReqID)
		return
	}
	services := cfg.Services.Enabled
	if req.Service != "" {
		cfg.Services.Enabled = nil
		if slices.Contains(services, req.Service) {
			cfg.Services.Enabled = []string{req.Service}
		}
	}
	meta := bubble.RunMetaMsg{ReqID: req.ReqID, SourceLang: request.SourceLang, TargetLang: request.TargetLang, Services: services}
	if !a.bubbles.PushMeta(win, meta, req.Service) {
		a.bubbles.FinishRun(win, req.ReqID)
		return
	}
	if req.Service != "" && !slices.Contains(services, req.Service) {
		msg := bubble.TranslationUpdateMsg{ReqID: req.ReqID, Service: req.Service, Err: "该服务当前不可用，请检查设置"}
		a.bubbles.PushResult(win, msg)
		a.bubbles.FinishRun(win, req.ReqID)
		return
	}
	go func() {
		defer a.bubbles.FinishRun(win, req.ReqID)
		emit := func(res translate.Result) {
			if ctx.Err() != nil {
				return
			}
			msg := bubble.TranslationUpdateMsg{
				ReqID: req.ReqID, Service: res.Service, Text: res.Text, DetectedLang: res.DetectedLang,
				Err: res.Err, Streaming: res.Streaming, Dictionary: res.Dictionary,
			}
			a.bubbles.PushResult(win, msg)
		}
		translate.Run(ctx, cfg, credentials, request, emit)
	}()
}

func prepareTranslationRequest(text, source, target string, cfg config.Settings) (translate.Request, error) {
	if err := translate.ValidateInput(text); err != nil {
		return translate.Request{}, err
	}
	text = strings.TrimSpace(translate.PrepareSource(text, cfg.Identifier))
	if text == "" {
		return translate.Request{}, translate.ErrEmptySource
	}
	if err := translate.ValidateInput(text); err != nil {
		return translate.Request{}, err
	}
	detected := langdetect.Detect(text)
	// Identifiers translate better as words: getUserInfo goes to the
	// services as "get User Info".
	if cfg.Identifier.AutoSplit && detected == "latin" && naming.DetectStyle(text) != naming.None {
		text = strings.Join(naming.SplitWords(text), " ")
		if err := translate.ValidateInput(text); err != nil {
			return translate.Request{}, err
		}
	}
	if source == "" {
		source = "auto"
	}
	return translate.Request{
		Text: text, SourceLang: source, TargetLang: resolveTarget(detected, target, cfg.TargetLanguage),
	}, nil
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
