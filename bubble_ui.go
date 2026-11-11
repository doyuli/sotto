package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
	"sotto/internal/languages"
	"sotto/internal/naming"
)

var (
	brandBubbleSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><rect x="2.5" y="2.8" width="19" height="16.8" rx="5.2" fill="currentColor"/><path d="M7 19.2 5.8 22.4 9 20.6" fill="currentColor"/></svg>`))
	brandLetterSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M15 8.2C14.3 7 12.4 6.8 11.25 7.5C9.84 8.34 9.6 9.73 10.9 10.6C12.07 11.39 13.6 11.39 14.3 12.2C15.23 13.24 14.65 15.23 13 15.8C11.72 16.3 10.31 15.8 9.49 14.88" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	pinOutlineSVG  = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M12 17v5M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	pinFilledSVG   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M12 17v5M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z" fill="currentColor" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	closeSVG       = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M18 6 6 18M6 6l12 12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	swapSVG        = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="m16 3 4 4-4 4M20 7H4M8 21l-4-4 4-4M4 17h16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	copySVG        = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><rect width="14" height="14" x="8" y="8" rx="2" ry="2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	chevronDownSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="m6 9 6 6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
	chevronLeftSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="m15 18-6-6 6-6" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
)

// bubblePalette mirrors the Sotto bubble tokens in DESIGN.md / style.css so the
// native bubble preserves the neutral surface ladder and blue accent across themes.
type bubblePalette struct {
	dark       bool
	panel      ui.Color
	control    ui.Color
	pill       ui.Color
	hover      ui.Color
	composer   ui.Color
	cardHead   ui.Color
	cardBody   ui.Color
	fg         ui.Color
	muted      ui.Color
	icon       ui.Color
	iconStrong ui.Color
	clearBg    ui.Color
	clearFg    ui.Color
	accent     ui.Color
	accentText ui.Color
	error      ui.Color
	selection  ui.Color
	theme      *ui.Theme
}

var lightBubblePalette = func() bubblePalette {
	p := bubblePalette{
		dark:       false,
		panel:      ui.Hex("#ffffff"),
		control:    ui.Hex("#f1f1f1"),
		pill:       ui.Hex("#e7e7e7"),
		hover:      ui.Hex("#e9e9e9"),
		composer:   ui.Hex("#f4f4f4"),
		cardHead:   ui.Hex("#f1f1f1"),
		cardBody:   ui.Hex("#f6f6f6"),
		fg:         ui.Hex("#202020"),
		muted:      ui.Hex("#5f5f5f"),
		icon:       ui.Hex("#4b4b4b"),
		iconStrong: ui.Hex("#202020"),
		clearBg:    ui.Hex("#c9c9c9"),
		clearFg:    ui.Hex("#4b4b4b"),
		accent:     ui.Hex("#2f7fe8"),
		accentText: ui.Hex("#1a5fae"),
		error:      ui.Hex("#c62828"),
		selection:  ui.RGBA(31, 111, 208, 0.18),
	}
	t := ui.LightTheme()
	t.Background = p.panel
	t.Surface = p.control
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pill
	t.Border = p.pill
	t.Text = p.fg
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = ui.Hex("#ffffff")
	t.Scrollbar = ui.Hex("#c9c9c9")
	t.ScrollbarWidth = 6
	t.Danger = p.error
	p.theme = t
	return p
}()

var darkBubblePalette = func() bubblePalette {
	p := bubblePalette{
		dark:       true,
		panel:      ui.Hex("#222325"),
		control:    ui.Hex("#2c2d2f"),
		pill:       ui.Hex("#3a3b3d"),
		hover:      ui.Hex("#3a3b3d"),
		composer:   ui.Hex("#303132"),
		cardHead:   ui.Hex("#2c2d2f"),
		cardBody:   ui.Hex("#303132"),
		fg:         ui.Hex("#f3f4f5"),
		muted:      ui.Hex("#a0a3a7"),
		icon:       ui.Hex("#b9bcc0"),
		iconStrong: ui.Hex("#f3f4f5"),
		clearBg:    ui.Hex("#55575b"),
		clearFg:    ui.Hex("#f3f4f5"),
		accent:     ui.Hex("#7db4ff"),
		accentText: ui.Hex("#7db4ff"),
		error:      ui.Hex("#ff8a8a"),
		selection:  ui.RGBA(125, 180, 255, 0.28),
	}
	t := ui.DarkTheme()
	t.Background = p.panel
	t.Surface = p.control
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pill
	t.Border = p.pill
	t.Text = p.fg
	t.TextMuted = p.muted
	t.Accent = p.accent
	t.AccentText = ui.Hex("#0d1b2b")
	t.Scrollbar = ui.Hex("#4a4c50")
	t.ScrollbarWidth = 6
	t.Danger = p.error
	p.theme = t
	return p
}()

func paletteForTheme(t *ui.Theme) bubblePalette {
	if t != nil && t.Dark {
		return darkBubblePalette
	}
	return lightBubblePalette
}

var serviceDisplayNames = map[string]string{
	"deepl":           "DeepL",
	"openai":          "OpenAI",
	"siliconflow":     "硅基流动",
	"zhipu":           "智谱",
	"baidu":           "百度翻译",
	"volcengine":      "火山引擎",
	"tencent":         "腾讯云",
	"youdao":          "有道",
	"azure":           "微软翻译",
	"google":          "Google",
	"index-translate": "Bilibili",
	"dictionary":      "词典",
}

func bubbleServiceName(service string) string {
	if name, ok := serviceDisplayNames[service]; ok {
		return name
	}
	return service
}

func bubbleLangName(code string) string {
	if code == "auto" {
		return "自动检测"
	}
	if name, ok := languages.Names[code]; ok {
		return name
	}
	return code
}

func bubbleDetectedName(code string) string {
	switch code {
	case "", "auto", "latin", "unknown":
		return "自动检测"
	case "zh":
		return bubbleLangName("zh-HANS")
	default:
		return bubbleLangName(code)
	}
}

func bubbleChipTag(style string) string {
	switch style {
	case naming.LowerCamel:
		return "aB"
	case naming.UpperCamel:
		return "AB"
	case naming.Snake:
		return "a_b"
	case naming.Constant:
		return "A_B"
	case naming.Kebab:
		return "a-b"
	default:
		return style
	}
}

// bubbleViewActions wires native bubble UI events back into the manager and OS
// integration hooks. Keeping callbacks explicit allows headless ui.NewTester tests.
type bubbleViewActions struct {
	run              func(req RunReq)
	cancel           func(sessionID string, revision int64)
	setPinned        func(sessionID string, showID int64, pinned bool) bool
	close            func(sessionID string, showID, revision int64) bool
	startOCR         func()
	openSettings     func()
	restoreClipboard func(id string) error
	discardClipboard func(id string)
	copyText         func(c *ui.Context, text string) error
	onHeightMeasured func(height float64)
}

type bubbleCard struct {
	Loading   bool
	Streaming bool
	Text      string
	Err       string
}

// bubbleView owns the native UI state for one bubble window on the UI thread.
type bubbleView struct {
	id                  int64
	generation          int64
	sessionID           string
	showID              int64
	layoutID            int64
	stateRevision       int64
	revision            int64
	reqID               string
	trigger             string
	phase               string
	message             string
	clipboardRecoveryID string
	pinned              bool
	maxHeight           float64

	settings      config.Settings
	sourceLang    string
	targetLang    string
	detected      string
	submittedText string
	composerText  string
	lastComposer  string
	retryService  string
	runServices   map[string]bool
	serviceReqIDs map[string]string
	cards         map[string]*bubbleCard
	collapsed     map[string]bool

	headerHandle   ui.Handle
	bodyHandle     ui.Handle
	composerHandle ui.Handle
	scroll         ui.ScrollState
	composerScroll ui.ScrollState
	bodyScrollbar  bubbleScrollbar
	inputScrollbar bubbleScrollbar

	pendingFocus      bool
	pendingScrollTop  bool
	resetScrollbars   bool
	contentVersion    uint64
	paintedVersion    uint64
	measuredVersion   uint64
	lastMeasuredH     float64
	lastMeasuredLimit float64

	actions bubbleViewActions
}

func newBubbleView(id int64, cfg config.Settings, actions bubbleViewActions) *bubbleView {
	return &bubbleView{
		id:            id,
		maxHeight:     bubbleMaxHeight,
		settings:      cfg,
		sourceLang:    "auto",
		targetLang:    cfg.TargetLanguage,
		serviceReqIDs: make(map[string]string),
		cards:         make(map[string]*bubbleCard),
		collapsed:     make(map[string]bool),
		actions:       actions,
	}
}

func (v *bubbleView) applyState(st BubbleState, focus bool) {
	if v.sessionID != "" && (st.Generation < v.generation ||
		(st.SessionID == v.sessionID && (st.Revision < v.stateRevision || st.ShowID < v.showID || st.LayoutID < v.layoutID))) {
		return
	}
	v.generation = st.Generation
	v.sessionID = st.SessionID
	v.showID = st.ShowID
	v.layoutID = st.LayoutID
	v.stateRevision = st.Revision
	if st.MaxHeight > 0 {
		v.maxHeight = st.MaxHeight
	}
	v.trigger = st.Trigger
	v.phase = st.Phase
	v.message = st.Message
	v.clipboardRecoveryID = st.ClipboardRecoveryID
	v.pinned = st.Pinned
	v.settings = st.Settings

	v.reqID = st.ReqID
	v.runServices = nil
	v.detected = ""
	v.revision = max(0, st.Revision)
	v.submittedText = st.Text
	v.composerText = st.Text
	v.lastComposer = st.Text
	v.retryService = ""
	if st.SourceLang != "" {
		v.sourceLang = st.SourceLang
	} else {
		v.sourceLang = "auto"
	}
	if st.TargetLang != "" {
		v.targetLang = st.TargetLang
	} else if st.Settings.TargetLanguage != "" {
		v.targetLang = st.Settings.TargetLanguage
	} else {
		v.targetLang = "zh-HANS"
	}

	v.cards = make(map[string]*bubbleCard)
	v.serviceReqIDs = make(map[string]string)
	for svc, id := range st.ServiceRequests {
		v.serviceReqIDs[svc] = id
	}
	if st.ReqID != "" && st.Text != "" {
		for _, svc := range v.activeConfiguredServices() {
			v.cards[svc] = &bubbleCard{Loading: true}
		}
	}
	if st.Meta != nil && st.Meta.ReqID == st.ReqID {
		v.applyMetaLocked(st.Meta)
	}
	for _, msg := range st.Results {
		v.applyResultInternal(msg)
	}

	v.pendingFocus = focus || st.Trigger == "input"
	v.pendingScrollTop = true
	v.resetScrollbars = true
	v.composerScroll.X, v.composerScroll.Y = 0, 0
	v.inputScrollbar.reset(&v.composerScroll)
	v.lastMeasuredH = 0
	v.bumpContent()
}

func (v *bubbleView) onPresented(msg BubblePresentedMsg) {
	if msg.SessionID != v.sessionID || msg.ShowID < v.showID {
		return
	}
	v.clipboardRecoveryID = msg.ClipboardRecoveryID
	v.showID = msg.ShowID
	if msg.Revision > v.revision {
		v.stopRenderedRun(msg.Revision)
	}
	if msg.Focus {
		v.pendingFocus = true
	}
	v.bumpContent()
}

func (v *bubbleView) onStopped(msg RunStoppedMsg) {
	if msg.SessionID != v.sessionID {
		return
	}
	if msg.ShowID > v.showID {
		v.showID = msg.ShowID
	}
	if msg.Revision > v.revision {
		v.stopRenderedRun(msg.Revision)
		v.bumpContent()
	}
}

func (v *bubbleView) stopRenderedRun(nextRevision int64) {
	v.revision = nextRevision
	v.stateRevision = nextRevision
	v.reqID = ""
	v.retryService = ""
	v.serviceReqIDs = make(map[string]string)
	for _, card := range v.cards {
		if card.Loading || card.Streaming {
			card.Loading = false
			card.Streaming = false
			if card.Err == "" {
				card.Err = "翻译已取消，请重试"
			}
		}
	}
}

func (v *bubbleView) applyMeta(meta RunMetaMsg) {
	if meta.ReqID != v.reqID {
		for svc, reqID := range v.serviceReqIDs {
			if reqID == meta.ReqID && !slices.Contains(meta.Services, svc) {
				v.cards[svc] = &bubbleCard{Err: "该服务当前不可用，请检查设置"}
			}
		}
		v.bumpContent()
		return
	}
	v.applyMetaLocked(&meta)
	v.bumpContent()
}

func (v *bubbleView) applyMetaLocked(meta *RunMetaMsg) {
	if meta.SourceLang != "" {
		v.sourceLang = meta.SourceLang
	}
	if meta.TargetLang != "" {
		v.targetLang = meta.TargetLang
	}
	if v.retryService == "" {
		v.runServices = make(map[string]bool, len(meta.Services))
		for _, svc := range meta.Services {
			v.runServices[svc] = true
			v.serviceReqIDs[svc] = meta.ReqID
		}
		for svc := range v.cards {
			if !v.runServices[svc] {
				delete(v.cards, svc)
			}
		}
	} else if !slices.Contains(meta.Services, v.retryService) {
		v.cards[v.retryService] = &bubbleCard{Err: "该服务当前不可用，请检查设置"}
	}
}

func (v *bubbleView) applyResult(msg TranslationUpdateMsg) {
	expected := v.reqID
	if id, ok := v.serviceReqIDs[msg.Service]; ok {
		expected = id
	}
	if msg.ReqID != expected {
		return
	}
	v.applyResultInternal(msg)
	v.bumpContent()
}

func (v *bubbleView) applyResultInternal(msg TranslationUpdateMsg) {
	if v.sourceLang == "auto" && msg.DetectedLang != "" &&
		msg.DetectedLang != "latin" && msg.DetectedLang != "unknown" {
		if v.detected == "" {
			v.detected = msg.DetectedLang
		}
	}
	prev := v.cards[msg.Service]
	if msg.Streaming {
		v.cards[msg.Service] = &bubbleCard{
			Streaming: true,
			Text:      msg.Text,
		}
		return
	}
	if msg.Err != "" {
		text := msg.Text
		if text == "" && prev != nil {
			text = prev.Text
		}
		v.cards[msg.Service] = &bubbleCard{
			Text: text,
			Err:  msg.Err,
		}
		return
	}
	v.cards[msg.Service] = &bubbleCard{
		Text: msg.Text,
	}
}

func (v *bubbleView) applySettings(s config.Settings) {
	v.settings = s
	v.bumpContent()
}

func (v *bubbleView) setMaxHeight(limit float64) {
	if limit > 0 {
		v.maxHeight = limit
		v.bumpContent()
	}
}

func (v *bubbleView) bumpContent() {
	v.contentVersion++
}

func (v *bubbleView) isRecognizing() bool {
	return v.phase == "recognizing"
}

func (v *bubbleView) isDirty() bool {
	return v.submittedText != "" && strings.TrimSpace(v.composerText) != strings.TrimSpace(v.submittedText)
}

func (v *bubbleView) statusMessage() string {
	if v.isRecognizing() {
		return "正在识别文字…"
	}
	if v.isDirty() {
		return "原文已修改，按 Enter 重新翻译"
	}
	if v.message != "" {
		return v.message
	}
	if v.trigger == "ocr" {
		return "识别文字可直接修改，按 Enter 重新翻译"
	}
	return ""
}

func (v *bubbleView) detectedDisplay() string {
	src := v.detected
	if src == "" {
		src = v.sourceLang
	}
	if src != "" && src != "auto" && src != "latin" && src != "unknown" {
		return bubbleDetectedName(src)
	}
	return ""
}

func (v *bubbleView) activeConfiguredServices() []string {
	enabled := make(map[string]bool, len(v.settings.Services.Enabled))
	for _, s := range v.settings.Services.Enabled {
		enabled[s] = true
	}
	out := make([]string, 0, len(v.settings.Services.Order))
	for _, name := range v.settings.Services.Order {
		if enabled[name] {
			out = append(out, name)
		}
	}
	return out
}

func (v *bubbleView) enabledServices() []string {
	all := v.activeConfiguredServices()
	if v.runServices == nil {
		return all
	}
	out := make([]string, 0, len(all))
	for _, name := range all {
		if v.runServices[name] {
			out = append(out, name)
		}
	}
	return out
}

func (v *bubbleView) showingServices() []string {
	enabled := v.enabledServices()
	out := make([]string, 0, len(enabled))
	for _, name := range enabled {
		if _, ok := v.cards[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

func (v *bubbleView) canSwapLanguages() bool {
	return !(v.sourceLang == "auto" && v.detected == "")
}

func (v *bubbleView) startRun(text string, service string) {
	trimmed := strings.TrimSpace(text)
	if v.sessionID == "" || v.isRecognizing() {
		return
	}
	if trimmed == "" {
		if v.actions.cancel != nil {
			v.revision++
			v.actions.cancel(v.sessionID, v.revision)
		}
		v.reqID = ""
		v.retryService = ""
		v.runServices = nil
		v.cards = make(map[string]*bubbleCard)
		v.serviceReqIDs = make(map[string]string)
		v.submittedText = ""
		v.detected = ""
		v.phase = ""
		v.message = ""
		v.bumpContent()
		return
	}
	// A retry after editing is a new full run; every card must match the draft.
	if service != "" && trimmed != strings.TrimSpace(v.submittedText) {
		service = ""
	}

	v.revision++
	v.stateRevision = v.revision
	nextReqID := newRequestID()
	v.reqID = nextReqID
	v.phase = ""
	v.message = ""

	if service == "" {
		v.retryService = ""
		v.submittedText = text
		v.runServices = nil
		v.detected = ""
		v.cards = make(map[string]*bubbleCard)
		v.serviceReqIDs = make(map[string]string)
		for _, svc := range v.activeConfiguredServices() {
			v.cards[svc] = &bubbleCard{Loading: true}
			v.serviceReqIDs[svc] = nextReqID
		}
	} else {
		v.retryService = service
		v.cards[service] = &bubbleCard{Loading: true}
		v.serviceReqIDs[service] = nextReqID
	}
	v.bumpContent()

	if v.actions.run != nil {
		sourceTrigger := "input"
		if service != "" && v.trigger != "" {
			sourceTrigger = v.trigger
		}
		v.actions.run(RunReq{
			ReqID:      nextReqID,
			SessionID:  v.sessionID,
			Revision:   v.revision,
			Service:    service,
			Source:     sourceTrigger,
			Text:       text,
			SourceLang: v.sourceLang,
			TargetLang: v.targetLang,
		})
	}
}

func (v *bubbleView) submitComposer(text string) {
	v.composerText = text
	v.lastComposer = text
	v.startRun(text, "")
}

func (v *bubbleView) clearComposer(c *ui.Context) {
	v.composerText = ""
	v.lastComposer = ""
	if v.actions.cancel != nil && v.sessionID != "" {
		v.revision++
		v.actions.cancel(v.sessionID, v.revision)
	}
	v.reqID = ""
	v.retryService = ""
	v.cards = make(map[string]*bubbleCard)
	v.serviceReqIDs = make(map[string]string)
	v.submittedText = ""
	v.detected = ""
	v.phase = ""
	v.message = ""
	v.composerHandle.Focus(c)
	v.bumpContent()
}

func (v *bubbleView) setSourceLang(code string) {
	if v.sourceLang == code || v.isRecognizing() {
		return
	}
	v.sourceLang = code
	v.detected = ""
	if strings.TrimSpace(v.composerText) != "" {
		v.startRun(v.composerText, "")
	} else {
		v.bumpContent()
	}
}

func (v *bubbleView) setTargetLang(code string) {
	if v.targetLang == code || v.isRecognizing() {
		return
	}
	v.targetLang = code
	if strings.TrimSpace(v.composerText) != "" {
		v.startRun(v.composerText, "")
	} else {
		v.bumpContent()
	}
}

func (v *bubbleView) swapLanguages() {
	if v.isRecognizing() || !v.canSwapLanguages() {
		return
	}
	nextTarget := v.sourceLang
	if nextTarget == "auto" {
		if v.detected == "" || v.detected == "latin" || v.detected == "unknown" {
			return
		}
		nextTarget = v.detected
		if nextTarget == "zh" {
			nextTarget = "zh-HANS"
		}
	}
	if !languages.Valid(nextTarget) {
		return
	}
	v.sourceLang = v.targetLang
	v.targetLang = nextTarget
	v.detected = ""
	if strings.TrimSpace(v.composerText) != "" {
		v.startRun(v.composerText, "")
	} else {
		v.bumpContent()
	}
}

func (v *bubbleView) togglePin() {
	if v.isRecognizing() || v.sessionID == "" {
		return
	}
	next := !v.pinned
	if v.actions.setPinned == nil || v.actions.setPinned(v.sessionID, v.showID, next) {
		v.pinned = next
		v.bumpContent()
	}
}

func (v *bubbleView) closeBubble() {
	if v.actions.close != nil {
		v.revision++
		v.actions.close(v.sessionID, v.showID, v.revision)
	}
}

func (v *bubbleView) copyAndToast(c *ui.Context, text string, toast string) {
	if v.actions.copyText != nil {
		if err := v.actions.copyText(c, text); err != nil {
			c.Toast(err.Error())
			return
		}
	} else {
		c.WriteClipboard(text)
	}
	c.Toast(toast)
}

func (v *bubbleView) render(c *ui.Context) {
	p := paletteForTheme(c.Theme())
	c.SetTheme(p.theme)

	if v.composerText != v.lastComposer {
		v.lastComposer = v.composerText
		if strings.TrimSpace(v.composerText) == "" {
			v.startRun("", "")
		} else {
			v.bumpContent()
		}
	}
	if v.pendingScrollTop {
		v.pendingScrollTop = false
		v.scroll.Y = 0
		v.bodyScrollbar.reset(&v.scroll)
	}
	if c.Shortcut(0, ui.KeyEscape) {
		v.closeBubble()
	}

	c.Root().Background(p.panel).Padding(0).Children(func() {
		ui.Column(c).FillHeight().Grow(1).Children(func() {
			v.renderHeader(c, p)
			theme := v.bodyScrollbar.themed(c, &v.scroll, p.theme)
			ui.Themed(c, theme).Grow(1).Children(func() {
				ui.Scroll(c).TrackScroll(&v.scroll).KeepScrollbars(true).Grow(1).Children(func() {
					ui.Column(c).Padding(2, 12, 12, 12).Gap(8).Bind(&v.bodyHandle).Children(func() {
						v.renderBanners(c, p)
						v.renderComposer(c, p)
						v.renderLanguageBar(c, p)
						v.renderResults(c, p)
					})
				})
			})
		})
	})

	// Bounds belong to the last committed frame, not the previous build pass.
	// Paint acknowledges that this content version has actually been laid out.
	if v.paintedVersion == v.contentVersion && v.measuredVersion != v.paintedVersion {
		hdr := v.headerHandle.Bounds(c)
		body := v.bodyHandle.Bounds(c)
		if body.H > 0 {
			v.measuredVersion = v.paintedVersion
			total := float64(hdr.H + body.H)
			if total != v.lastMeasuredH || v.maxHeight != v.lastMeasuredLimit {
				v.lastMeasuredH, v.lastMeasuredLimit = total, v.maxHeight
				if v.actions.onHeightMeasured != nil {
					v.actions.onHeightMeasured(total)
				}
			}
		}
	}
	version := v.contentVersion
	services := c.Services()
	c.Root().DrawOver(func(_ *ui.Painter, _ ui.Rect) {
		v.paintedVersion = version
		if v.resetScrollbars {
			// Initial caret placement is layout, not a user scroll gesture.
			v.resetScrollbars = false
			v.bodyScrollbar.reset(&v.scroll)
			v.inputScrollbar.reset(&v.composerScroll)
		}
		// Caret reveal can scroll the editor during layout, after the theme
		// was chosen. Rebuild once to reveal its scrollbar without another input.
		if v.measuredVersion != version ||
			v.bodyScrollbar.x != v.scroll.X || v.bodyScrollbar.y != v.scroll.Y ||
			v.inputScrollbar.x != v.composerScroll.X || v.inputScrollbar.y != v.composerScroll.Y {
			services.Invalidate()
		}
	})
}

func (v *bubbleView) renderHeader(c *ui.Context, p bubblePalette) {
	ui.Row(c).Height(30).Padding(6, 8, 2, 12).AlignItems(ui.Center).DragWindow().Bind(&v.headerHandle).Children(func() {
		ui.Box(c).Size(20, 20).Children(func() {
			ui.Icon(c, brandBubbleSVG).Size(20, 20).TextColor(p.accent)
			ui.Box(c).Absolute().Top(0).Left(0).Size(20, 20).Children(func() {
				ui.Icon(c, brandLetterSVG).Size(20, 20).TextColor(p.panel)
			})
		})
		ui.Spacer(c)

		ui.Row(c).AlignItems(ui.Center).Gap(2).Children(func() {
			pinTip := "钉住：失焦不隐藏，之后的新翻译另开气泡"
			if v.pinned {
				pinTip = "已钉住：点击取消，气泡恢复失焦隐藏"
			}
			pinBtn := ui.ButtonBase(c).
				Label("钉住").
				Tooltip(pinTip).
				Disabled(v.isRecognizing()).
				Cursor(ui.CursorPointer).
				Size(24, 24).
				Radius(6).
				Center()
			if !v.isRecognizing() && pinBtn.Hovered() {
				pinBtn.Background(p.hover)
			}
			if pinBtn.Clicked() {
				v.togglePin()
			}
			pinBtn.Children(func() {
				color := p.icon
				if v.pinned {
					color = p.accent
					ui.Icon(c, pinFilledSVG).Size(15, 15).TextColor(color)
					return
				}
				if v.isRecognizing() {
					color = p.muted.Alpha(0.45)
				} else if pinBtn.Hovered() {
					color = p.iconStrong
				}
				ui.Icon(c, pinOutlineSVG).Size(15, 15).TextColor(color)
			})

			closeBtn := ui.ButtonBase(c).
				Label("关闭").
				Tooltip("关闭 (Esc)").
				Cursor(ui.CursorPointer).
				Size(24, 24).
				Radius(6).
				Center()
			if closeBtn.Hovered() {
				closeBtn.Background(p.hover)
			}
			if closeBtn.Clicked() {
				v.closeBubble()
			}
			closeBtn.Children(func() {
				color := p.icon
				if closeBtn.Hovered() {
					color = p.iconStrong
				}
				ui.Icon(c, closeSVG).Size(15, 15).TextColor(color)
			})
		})
	})
}

func (v *bubbleView) renderLinkButton(c *ui.Context, p bubblePalette, label string, onClick func()) {
	btn := ui.ButtonBase(c).Label(label).Padding(2, 2).Radius(4).Cursor(ui.CursorPointer)
	if btn.Clicked() && onClick != nil {
		onClick()
	}
	btn.Children(func() {
		txt := ui.Text(c, label).FontSize(12).TextColor(p.accentText).SingleLine()
		if btn.Hovered() {
			txt.Underline()
		}
	})
}

func (v *bubbleView) renderBanners(c *ui.Context, p bubblePalette) {
	status := v.statusMessage()
	if status != "" || v.clipboardRecoveryID != "" {
		ui.Column(c).Padding(2, 2).Gap(4).Children(func() {
			if status != "" {
				isError := v.phase == "error" && !v.isDirty() && !v.isRecognizing()
				textColor := p.muted
				if isError {
					textColor = p.error
				}
				ui.Row(c).AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Grow(1).Children(func() {
						if v.isRecognizing() {
							ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
								for i := range 3 {
									dot := ui.Box(c).Size(4, 4).Radius(2).Background(p.muted)
									phase := float32(i) * 0.25
									loop := dot.Loop("ocr-dot", 900*time.Millisecond, ui.Bounce(ui.EaseInOut))
									alpha := 0.35 + 0.65*float32((loop+phase)-float32(int(loop+phase)))
									dot.Opacity(alpha)
								}
							})
						}
						ui.Text(c, status).FontSize(12).TextColor(textColor)
					})
					if v.isRecognizing() {
						v.renderLinkButton(c, p, "取消识别", v.closeBubble)
					} else if v.trigger == "ocr" {
						v.renderLinkButton(c, p, "重新框选", v.actions.startOCR)
					} else if isError {
						v.renderLinkButton(c, p, "截图翻译", v.actions.startOCR)
					}
				})
			}

			if v.clipboardRecoveryID != "" {
				id := v.clipboardRecoveryID
				ui.Column(c).Padding(8, 10).Radius(10).Background(p.control).Gap(6).Children(func() {
					ui.Text(c, "之前复制的内容尚未恢复，可重试或放弃后继续取词。").FontSize(12).TextColor(p.muted)
					ui.Row(c).Gap(12).Children(func() {
						v.renderLinkButton(c, p, "重试恢复剪贴板", func() {
							if v.actions.restoreClipboard == nil {
								return
							}
							if err := v.actions.restoreClipboard(id); err != nil {
								c.Toast(err.Error())
							} else {
								v.clipboardRecoveryID = ""
								v.bumpContent()
								c.Toast("已恢复剪贴板内容")
							}
						})
						v.renderLinkButton(c, p, "放弃恢复，继续取词", func() {
							if v.actions.discardClipboard != nil {
								v.actions.discardClipboard(id)
							}
							v.clipboardRecoveryID = ""
							v.bumpContent()
						})
					})
				})
			}
		})
	}
}

func (v *bubbleView) renderComposer(c *ui.Context, p bubblePalette) {
	placeholder := "输入要翻译的文本，回车翻译…"
	if v.isRecognizing() {
		placeholder = "识别完成后可修改原文"
	}
	border := p.composer
	if v.composerHandle.Focused(c) {
		border = p.accent
	}
	box := ui.Column(c.Key("composer")).Radius(12).Background(p.composer).Border(1, border).Padding(9, 11, 5, 11).Gap(4)
	box.Children(func() {
		theme := v.inputScrollbar.themed(c, &v.composerScroll, p.theme)
		ui.Themed(c, theme).Background(ui.Transparent).Children(func() {
			ui.TextAreaBase(c, &v.composerText).
				Bind(&v.composerHandle).
				TrackScroll(&v.composerScroll).
				KeepScrollbars(true).
				Label("要翻译的原文").
				Lines(2, 5).
				Padding(2, 2, 4, 2).
				Placeholder(placeholder).
				PlaceholderColor(p.muted.Alpha(0.85)).
				TextColor(p.fg).
				CaretColor(p.accent).
				SelectionColor(p.selection).
				ReadOnly(v.isRecognizing()).
				FontSize(14.5).
				LineHeight(1.55).
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key == ui.KeyEscape && k.Mods == 0 {
						v.closeBubble()
						return true
					}
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						v.submitComposer(k.Text())
						return true
					}
					return false
				})
		})
		if v.pendingFocus {
			v.pendingFocus = false
			v.composerHandle.Focus(c)
		}

		hasText := strings.TrimSpace(v.composerText) != ""
		detected := v.detectedDisplay()
		if hasText || detected != "" || (v.composerText != "" && !v.isRecognizing()) {
			ui.Row(c).MinHeight(26).AlignItems(ui.Center).Gap(4).Children(func() {
				if hasText {
					copyBtn := ui.ButtonBase(c).
						Label("复制原文").
						Tooltip("复制原文").
						Cursor(ui.CursorPointer).
						Size(24, 24).
						Radius(6).
						Center()
					if copyBtn.Hovered() {
						copyBtn.Background(p.hover)
					}
					if copyBtn.Clicked() {
						v.copyAndToast(c, v.composerText, "已复制原文")
					}
					copyBtn.Children(func() {
						color := p.icon
						if copyBtn.Hovered() {
							color = p.iconStrong
						}
						ui.Icon(c, copySVG).Size(15, 15).TextColor(color)
					})
				}
				if detected != "" {
					ui.Row(c).Padding(3, 9).Radius(999).Background(p.pill).Children(func() {
						ui.RichText(c,
							ui.Span{Text: "识别为 ", Color: p.muted},
							ui.Span{Text: detected, Color: p.accentText},
						).FontSize(12).SingleLine()
					})
				}
				ui.Spacer(c)
				if v.composerText != "" && !v.isRecognizing() {
					clearBtn := ui.ButtonBase(c).
						Label("清空").
						Tooltip("清空").
						Cursor(ui.CursorPointer).
						Size(20, 20).
						Radius(10).
						Background(p.clearBg).
						Center()
					if clearBtn.Hovered() {
						clearBtn.Background(p.icon)
					}
					if clearBtn.Clicked() {
						v.clearComposer(c)
					}
					clearBtn.Children(func() {
						fg := p.clearFg
						if clearBtn.Hovered() && !p.dark {
							fg = p.panel
						}
						ui.Icon(c, closeSVG).Size(11, 11).TextColor(fg)
					})
				}
			})
		}
	})
}

func (v *bubbleView) renderLanguageBar(c *ui.Context, p bubblePalette) {
	ui.Row(c).Height(38).Padding(2, 6).Radius(12).Background(p.control).AlignItems(ui.Center).Gap(2).Children(func() {
		v.renderLanguageSelect(c.Key("source"), p, true)

		swapDisabled := v.isRecognizing() || !v.canSwapLanguages()
		swapBtn := ui.ButtonBase(c).
			Label("交换语言").
			Tooltip("交换语言").
			Disabled(swapDisabled).
			Cursor(ui.CursorPointer).
			Size(26, 26).
			Radius(6).
			Center()
		if !swapDisabled && swapBtn.Hovered() {
			swapBtn.Background(p.hover)
		}
		if swapBtn.Clicked() {
			v.swapLanguages()
		}
		swapBtn.Children(func() {
			color := p.icon
			if swapDisabled {
				color = p.muted.Alpha(0.4)
			} else if swapBtn.Hovered() {
				color = p.iconStrong
			}
			ui.Icon(c, swapSVG).Size(15, 15).TextColor(color)
		})

		v.renderLanguageSelect(c.Key("target"), p, false)
	})
}

func (v *bubbleView) renderLanguageSelect(c *ui.Context, p bubblePalette, source bool) {
	selected, label := &v.targetLang, "目标语言"
	if source {
		selected, label = &v.sourceLang, "源语言"
	}
	before := *selected
	selectParts := ui.SelectBase(c, selected)
	trigger := selectParts.Trigger.Label(label).Disabled(v.isRecognizing()).
		Cursor(ui.CursorPointer).Grow(1).Basis(0).MinWidth(0).FillHeight().
		Radius(9).Padding(0, 8).Center()
	trigger.OnChange(func() {
		// The base has already updated the bound field before this notice.
		code := *selected
		*selected = before
		if source {
			v.setSourceLang(code)
		} else {
			v.setTargetLang(code)
		}
	})
	if !v.isRecognizing() && trigger.Hovered() {
		trigger.Background(p.hover)
	}
	trigger.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, bubbleLangName(*selected)).FontSize(13).TextColor(p.fg).SingleLine()
			ui.Icon(c, chevronDownSVG).Size(12, 12).TextColor(p.muted)
		})
	})
	selectParts.Popup(func(panel ui.Element) {
		// Match the committed trigger width; the select anchors the panel's
		// start edge and flips it above the control when space is limited.
		panel.Label(label+"选项").Width(trigger.Bounds().W).Padding(4).
			Margin(4, 0, 0, 0).Radius(10).Background(p.panel).
			Border(1, p.pill).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.14))
		codes := languages.List
		if source {
			codes = append([]string{"auto"}, codes...)
		}
		_, windowH := c.Size()
		bounds := trigger.Bounds()
		room := max(bounds.Y-8, windowH-bounds.Y-bounds.H-8)
		ui.Scroll(c).Height(min(float32(len(codes))*30, max(30, room-10))).Children(func() {
			for _, code := range codes {
				item := selectParts.Item(code).Height(30).Padding(0, 8).Radius(6).Cursor(ui.CursorPointer)
				if item.Highlighted() {
					item.Background(p.hover)
				} else if code == *selected {
					item.Background(p.control)
				}
				item.Children(func() {
					ui.Text(c, bubbleLangName(code)).FontSize(13).TextColor(p.fg).SingleLine()
				})
			}
		})
	})
}

func (v *bubbleView) renderResults(c *ui.Context, p bubblePalette) {
	showing := v.showingServices()
	if len(showing) == 0 {
		if !v.isRecognizing() && v.submittedText != "" && len(v.enabledServices()) == 0 {
			ui.Row(c).Padding(12, 0).Gap(12).Wrap().Center().Children(func() {
				ui.Text(c, "没有可用的翻译服务").FontSize(12).TextColor(p.muted)
				v.renderLinkButton(c, p, "配置翻译服务", v.actions.openSettings)
			})
		}
		return
	}

	ui.Column(c).Gap(8).Children(func() {
		for _, svc := range showing {
			card := v.cards[svc]
			if card == nil {
				continue
			}
			v.renderCard(c.Key("card:"+svc), p, svc, card)
		}
	})
}

func (v *bubbleView) renderCard(c *ui.Context, p bubblePalette, svc string, card *bubbleCard) {
	collapsed := v.collapsed[svc]
	title := bubbleServiceName(svc)
	ui.Column(c).Radius(12).Clip().Background(p.cardBody).Children(func() {
		header := ui.ButtonBase(c.Key("header")).
			Label("折叠 "+title).
			Expanded(!collapsed).
			Padding(7, 10, 7, 14).
			Background(p.cardHead).
			AlignItems(ui.Center).
			Gap(6).
			Cursor(ui.CursorPointer).
			OnClick(func() {
				v.collapsed[svc] = !v.collapsed[svc]
				v.bumpContent()
			})
		header.Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(500).TextColor(p.fg.Alpha(0.9)).SingleLine()
			ui.Spacer(c)
			if card.Streaming {
				ui.Text(c, "生成中").FontSize(12).TextColor(p.muted).SingleLine()
			}
			ui.Box(c).Size(24, 24).Center().Children(func() {
				color := p.muted
				if header.Hovered() {
					color = p.fg
				}
				if collapsed {
					ui.Icon(c, chevronLeftSVG).Size(14, 14).TextColor(color)
				} else {
					ui.Icon(c, chevronDownSVG).Size(14, 14).TextColor(color)
				}
			})
		})

		if collapsed {
			return
		}

		ui.Column(c).Padding(12, 14, 10, 14).Gap(6).Children(func() {
			switch {
			case card.Loading && card.Text == "":
				shimmer := p.pill
				pulse := func(el ui.Element) float32 {
					return 0.45 + 0.5*el.Loop("skel", 1200*time.Millisecond, ui.Bounce(ui.EaseInOut))
				}
				ui.Column(c).Padding(4, 0).Gap(8).Children(func() {
					b1 := ui.Box(c).Height(13).WidthPercent(100).Radius(6).Background(shimmer)
					b1.Opacity(pulse(b1))
					b2 := ui.Box(c).Height(13).WidthPercent(55).Radius(6).Background(shimmer)
					b2.Opacity(pulse(b2))
				})
			case card.Text != "":
				ui.Text(c, card.Text).
					Selectable().
					FontSize(15).
					LineHeight(1.65).
					TextColor(p.fg).
					SelectionColor(p.selection)
			}

			if card.Err != "" {
				errMsg := card.Err
				if card.Text != "" {
					errMsg = "译文未完成。" + errMsg
				}
				ui.Text(c, errMsg).FontSize(12).TextColor(p.error)
				ui.Row(c).Gap(12).Wrap().Children(func() {
					v.renderLinkButton(c, p, "重试", func() {
						v.startRun(v.composerText, svc)
					})
					if svc == "google" || svc == "index-translate" || strings.Contains(card.Err, "配置") {
						v.renderLinkButton(c, p, "配置翻译服务", v.actions.openSettings)
					}
				})
			}

			if card.Text != "" {
				ready := !card.Loading && !card.Streaming && card.Err == ""
				ui.Row(c).Padding(4, 0, 0, 0).AlignItems(ui.Center).Wrap().Gap(2).Children(func() {
					if ready && v.targetLang == "en" && len(v.settings.CopyFormats) > 0 {
						words := naming.SplitWords(card.Text)
						if len(words) > 0 {
							for _, style := range v.settings.CopyFormats {
								val := naming.Format(words, style)
								if val == "" {
									continue
								}
								label := naming.Labels[style]
								tag := bubbleChipTag(style)
								chip := ui.ButtonBase(c).
									Tooltip(label+"："+val).
									Cursor(ui.CursorPointer).
									MinWidth(24).
									Height(24).
									Padding(0, 5).
									Radius(6).
									Center()
								if chip.Hovered() {
									chip.Background(p.hover)
								}
								if chip.Clicked() {
									v.copyAndToast(c, val, "已复制 "+label)
								}
								chip.Children(func() {
									color := p.icon
									if chip.Hovered() {
										color = p.iconStrong
									}
									ui.Text(c, tag).
										Font("monospace").
										FontSize(10).
										LetterSpacing(-0.2).
										TextColor(color).
										SingleLine()
								})
							}
						}
					}

					copyTip := "译文完成后可复制"
					if ready {
						copyTip = "复制"
					}
					copyBtn := ui.ButtonBase(c).
						Label("复制 "+title).
						Tooltip(copyTip).
						Disabled(!ready).
						Cursor(ui.CursorPointer).
						Size(24, 24).
						Radius(6).
						Center()
					if ready && copyBtn.Hovered() {
						copyBtn.Background(p.hover)
					}
					if copyBtn.Clicked() {
						v.copyAndToast(c, card.Text, "已复制")
					}
					copyBtn.Children(func() {
						color := p.icon
						if !ready {
							color = p.icon.Alpha(0.45)
						} else if copyBtn.Hovered() {
							color = p.iconStrong
						}
						ui.Icon(c, copySVG).Size(14, 14).TextColor(color)
					})
				})
			}
		})
	})
}
