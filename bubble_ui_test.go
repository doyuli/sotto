package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
)

func testBubbleConfig() config.Settings {
	cfg := config.Default()
	cfg.Services.Enabled = []string{"google", "baidu"}
	cfg.Services.Order = []string{"google", "baidu"}
	cfg.CopyFormats = []string{"lowerCamel", "snake"}
	return cfg
}

func TestBubbleViewComposerSubmitAndClear(t *testing.T) {
	var runs []RunReq
	var canceled []int64
	var closed bool

	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		run:    func(req RunReq) { runs = append(runs, req) },
		cancel: func(_ string, rev int64) { canceled = append(canceled, rev) },
		close:  func(_ string, _, _ int64) bool { closed = true; return true },
	})
	v.applyState(BubbleState{
		ID:         1,
		Generation: 1,
		SessionID:  "sess-1",
		ShowID:     1,
		Revision:   0,
		Trigger:    "input",
		SourceLang: "auto",
		TargetLang: "zh-HANS",
		Settings:   cfg,
	}, true)

	tt := ui.NewTester(v.render, 480, 420)
	tt.Type("hello")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("world")
	if len(runs) != 0 {
		t.Fatalf("Shift+Enter submitted unexpectedly: %+v", runs)
	}
	tt.Key(0, ui.KeyEnter)
	if len(runs) != 1 || runs[0].Text != "hello\nworld" || runs[0].SessionID != "sess-1" {
		t.Fatalf("Enter submit = %+v, want text hello\\nworld", runs)
	}

	if err := tt.Click("复制原文"); err != nil {
		t.Fatalf("Click 复制原文: %v", err)
	}
	if got := tt.Clipboard(); got != "hello\nworld" {
		t.Fatalf("clipboard = %q, want %q", got, "hello\nworld")
	}

	if err := tt.Click("清空"); err != nil {
		t.Fatalf("Click 清空: %v", err)
	}
	if len(canceled) != 1 || v.composerText != "" {
		t.Fatalf("clear failed: canceled=%v text=%q", canceled, v.composerText)
	}

	tt.Key(0, ui.KeyEscape)
	if !closed {
		t.Fatal("Escape did not invoke close")
	}
}

func TestBubbleViewStreamingErrorRetryAndNamingChips(t *testing.T) {
	var runs []RunReq
	var openedSettings int

	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		run:          func(req RunReq) { runs = append(runs, req) },
		openSettings: func() { openedSettings++ },
	})
	v.applyState(BubbleState{
		ID:         1,
		Generation: 1,
		SessionID:  "sess-1",
		ShowID:     1,
		Revision:   0,
		ReqID:      "req-1",
		Trigger:    "selection",
		Text:       "用户名",
		SourceLang: "auto",
		TargetLang: "en",
		Settings:   cfg,
	}, false)

	tt := ui.NewTester(v.render, 480, 560)
	v.applyMeta(RunMetaMsg{
		ReqID:      "req-1",
		SourceLang: "auto",
		TargetLang: "en",
		Services:   []string{"google", "baidu"},
	})
	v.applyResult(TranslationUpdateMsg{
		ReqID:        "req-1",
		Service:      "google",
		Text:         "user",
		DetectedLang: "zh-HANS",
		Streaming:    true,
	})
	tt.Frame()
	if !tt.HasText("生成中") {
		t.Fatal("expected streaming badge while Streaming=true")
	}
	if tt.HasText("aB") {
		t.Fatal("naming chips rendered prematurely while streaming")
	}

	v.applyResult(TranslationUpdateMsg{
		ReqID:        "req-1",
		Service:      "google",
		Text:         "user name",
		DetectedLang: "zh-HANS",
		Streaming:    false,
	})
	v.applyResult(TranslationUpdateMsg{
		ReqID:   "req-1",
		Service: "baidu",
		Err:     "请先在设置中配置密钥",
	})
	tt.Frame()

	if !tt.HasText("识别为 中文（简体）") {
		t.Fatalf("missing detected badge, texts=%v", tt.Texts())
	}
	if err := tt.Click("aB"); err != nil {
		t.Fatalf("Click chip: %v", err)
	}
	if got := tt.Clipboard(); got != "userName" {
		t.Fatalf("clipboard = %q, want userName", got)
	}

	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatalf("collapse Google: %v", err)
	}
	if tt.HasText("user name") {
		t.Fatal("collapsed Google card body still visible")
	}
	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatalf("expand Google: %v", err)
	}
	if !tt.HasText("user name") {
		t.Fatal("expanded Google card body missing")
	}

	if err := tt.Click("配置翻译服务"); err != nil {
		t.Fatalf("Click 配置翻译服务: %v", err)
	}
	if openedSettings != 1 {
		t.Fatalf("openSettings count = %d, want 1", openedSettings)
	}

	if err := tt.Click("重试"); err != nil {
		t.Fatalf("Click 重试: %v", err)
	}
	if len(runs) != 1 || runs[0].Service != "baidu" {
		t.Fatalf("retry run = %+v, want baidu", runs)
	}
	if v.cards["baidu"] == nil || !v.cards["baidu"].Loading {
		t.Fatal("expected loading state after retry")
	}
}

func TestBubbleViewLanguageMenuAndSwap(t *testing.T) {
	var runs []RunReq

	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		run: func(req RunReq) { runs = append(runs, req) },
	})
	v.applyState(BubbleState{
		ID:         1,
		Generation: 1,
		SessionID:  "sess-1",
		ShowID:     1,
		Revision:   0,
		ReqID:      "req-1",
		Trigger:    "selection",
		Text:       "hello",
		SourceLang: "auto",
		TargetLang: "zh-HANS",
		Settings:   cfg,
	}, false)

	tt := ui.NewTester(v.render, 480, 420)
	if err := tt.Click("交换语言"); err != nil {
		t.Fatalf("Click disabled swap: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("swap ran while source was auto with no detection: %+v", runs)
	}

	r, ok := tt.Find("源语言")
	if !ok {
		t.Fatal("source language button not found")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	panel, ok := tt.Find("源语言选项")
	if !ok || panel.X != r.X || panel.W != r.W {
		t.Fatalf("source popup = %+v found=%v, want x=%v width=%v", panel, ok, r.X, r.W)
	}
	if err := tt.Click("英语"); err != nil {
		t.Fatalf("Click 英语: %v", err)
	}
	if len(runs) != 1 || runs[0].SourceLang != "en" || runs[0].TargetLang != "zh-HANS" {
		t.Fatalf("after choosing source=en runs=%+v", runs)
	}

	if err := tt.Click("交换语言"); err != nil {
		t.Fatalf("Click 交换语言: %v", err)
	}
	if len(runs) != 2 || runs[1].SourceLang != "zh-HANS" || runs[1].TargetLang != "en" {
		t.Fatalf("after swap runs=%+v", runs)
	}
}

func TestBubbleViewOCRPinAndClipboardRecovery(t *testing.T) {
	var pinnedCalls []bool
	var startOCRCalled int
	var discarded []string
	var closed bool
	restoreErr := errors.New("busy")

	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		setPinned:        func(_ string, _ int64, pinned bool) bool { pinnedCalls = append(pinnedCalls, pinned); return true },
		startOCR:         func() { startOCRCalled++ },
		discardClipboard: func(id string, done func()) { discarded = append(discarded, id); done() },
		restoreClipboard: func(_ string, done func(error)) { done(restoreErr) },
		close:            func(_ string, _, _ int64) bool { closed = true; return true },
	})
	v.applyState(BubbleState{
		ID:         1,
		Generation: 1,
		SessionID:  "sess-ocr",
		ShowID:     1,
		Revision:   -1,
		Trigger:    "ocr",
		Phase:      "recognizing",
		Settings:   cfg,
	}, false)

	tt := ui.NewTester(v.render, 480, 420)
	if !tt.HasText("正在识别文字…") {
		t.Fatal("missing recognizing banner")
	}
	if err := tt.Click("钉住"); err != nil {
		t.Fatalf("Click 钉住: %v", err)
	}
	if len(pinnedCalls) != 0 {
		t.Fatalf("pin allowed while recognizing: %v", pinnedCalls)
	}
	if err := tt.Click("取消识别"); err != nil {
		t.Fatalf("Click 取消识别: %v", err)
	}
	if !closed {
		t.Fatal("cancel OCR did not close bubble")
	}

	// OCR completion replaces progress within the same presentation.
	v.applyState(BubbleState{
		ID:         1,
		Generation: 2,
		SessionID:  "sess-ocr-2",
		ShowID:     2,
		Revision:   -1,
		Trigger:    "ocr",
		Phase:      "recognizing",
		Settings:   cfg,
	}, false)
	v.applyState(BubbleState{
		ID:                  1,
		Generation:          2,
		SessionID:           "sess-ocr-2",
		ShowID:              2,
		Revision:            -1,
		Trigger:             "ocr",
		Text:                "recognized",
		ClipboardRecoveryID: "rec-1",
		Settings:            cfg,
	}, false)
	tt.Frame()
	if !tt.HasText("识别文字可直接修改，按 Enter 重新翻译") {
		t.Fatal("missing OCR hint banner")
	}
	if err := tt.Click("重新框选"); err != nil {
		t.Fatalf("Click 重新框选: %v", err)
	}
	if startOCRCalled != 1 {
		t.Fatalf("startOCRCalled = %d, want 1", startOCRCalled)
	}

	if err := tt.Click("重试恢复剪贴板"); err != nil {
		t.Fatalf("Click 重试恢复剪贴板: %v", err)
	}
	if !tt.HasText("之前复制的内容尚未恢复，可重试或放弃后继续取词。") {
		t.Fatal("recovery banner dismissed on restore error")
	}
	restoreErr = nil
	if err := tt.Click("重试恢复剪贴板"); err != nil {
		t.Fatalf("Click 重试恢复剪贴板 (success): %v", err)
	}
	if tt.HasText("之前复制的内容尚未恢复，可重试或放弃后继续取词。") {
		t.Fatal("recovery banner remained after restore success")
	}

	v.clipboardRecoveryID = "rec-2"
	v.bumpContent()
	tt.Frame()
	if err := tt.Click("放弃恢复，继续取词"); err != nil {
		t.Fatalf("Click 放弃恢复，继续取词: %v", err)
	}
	if len(discarded) != 1 || discarded[0] != "rec-2" {
		t.Fatalf("discarded = %v, want [rec-2]", discarded)
	}

	if err := tt.Click("钉住"); err != nil {
		t.Fatalf("Click 钉住 after OCR: %v", err)
	}
	if len(pinnedCalls) != 1 || !pinnedCalls[0] {
		t.Fatalf("pinnedCalls = %v, want [true]", pinnedCalls)
	}
}

func TestBubbleViewComposerFocusAcrossDirtyBanner(t *testing.T) {
	var runs []RunReq
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		run: func(req RunReq) { runs = append(runs, req) },
	})
	v.applyState(BubbleState{
		ID: 1, Generation: 1, SessionID: "sess-focus", ShowID: 1,
		ReqID: "req-focus", Trigger: "selection", Text: "hello",
		SourceLang: "auto", TargetLang: "zh-HANS", Settings: cfg,
	}, false)
	v.applyMeta(RunMetaMsg{ReqID: "req-focus", Services: []string{"google"}})
	v.applyResult(TranslationUpdateMsg{ReqID: "req-focus", Service: "google", Text: "你好", DetectedLang: "en"})

	tt := ui.NewTester(v.render, 480, 560)
	const dirtyBanner = "原文已修改，按 Enter 重新翻译"
	if got := v.statusMessage(); got != "" {
		t.Fatalf("initial banner = %q, want none", got)
	}
	if err := tt.Click("要翻译的原文"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyRight)
	tt.Type("X")
	for range 3 {
		tt.Frame()
	}
	if !tt.HasText(dirtyBanner) {
		t.Errorf("dirty banner missing after edit, texts=%v", tt.Texts())
	}
	tt.Type("Y")
	if v.composerText != "hXYello" || !tt.Focused("要翻译的原文") {
		t.Errorf("editing across banner: text=%q focused=%v, want hXYello and true", v.composerText, tt.Focused("要翻译的原文"))
	}
	tt.Key(0, ui.KeyEnter)
	for range 3 {
		tt.Frame()
	}
	if len(runs) != 1 || runs[0].Text != "hXYello" || runs[0].Service != "" {
		t.Errorf("submit after editing = %+v, want one full run for hXYello", runs)
	}
	if tt.HasText(dirtyBanner) {
		t.Error("dirty banner remained after submit")
	}
	if !tt.Focused("要翻译的原文") {
		t.Error("composer lost focus when submit removed the dirty banner")
	}
}

func TestBubbleViewRetryUsesCurrentComposer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    bool
		text    string
		service string
	}{
		{name: "unchanged retries one service", text: "A", service: "google"},
		{name: "edited starts all services", edit: true, text: "B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var runs []RunReq
			cfg := testBubbleConfig()
			v := newBubbleView(1, cfg, bubbleViewActions{
				run: func(req RunReq) { runs = append(runs, req) },
			})
			v.applyState(BubbleState{
				ID: 1, Generation: 1, SessionID: "sess-retry", ShowID: 1,
				ReqID: "req-retry", Trigger: "selection", Text: "A",
				SourceLang: "auto", TargetLang: "zh-HANS", Settings: cfg,
			}, false)
			v.applyMeta(RunMetaMsg{ReqID: "req-retry", Services: []string{"google", "baidu"}})
			v.applyResult(TranslationUpdateMsg{ReqID: "req-retry", Service: "google", Err: "网络连接失败"})
			v.applyResult(TranslationUpdateMsg{ReqID: "req-retry", Service: "baidu", Text: "百度译文"})
			tt := ui.NewTester(v.render, 480, 760)
			if tc.edit {
				if err := tt.Click("要翻译的原文"); err != nil {
					t.Fatal(err)
				}
				tt.Key(ui.Ctrl, ui.KeyA)
				tt.Type("B")
				if v.composerText != "B" {
					t.Fatalf("composer = %q, want B before retry", v.composerText)
				}
			}
			if err := tt.Click("重试"); err != nil {
				t.Fatal(err)
			}
			if len(runs) != 1 {
				t.Fatalf("retry runs = %+v, want exactly one", runs)
			}
			if runs[0].Text != tc.text || runs[0].Service != tc.service {
				t.Errorf("retry = %+v, want text=%q service=%q", runs[0], tc.text, tc.service)
			}
			if !tc.edit && !tt.HasText("百度译文") {
				t.Error("single-service retry removed the other service's result")
			}
		})
	}
}

func TestBubbleViewKeyboardEmptyCancelsAndRejectsLateResult(t *testing.T) {
	type cancellation struct {
		session string
		rev     int64
	}
	var canceled []cancellation
	var runs []RunReq
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		run: func(req RunReq) { runs = append(runs, req) },
		cancel: func(session string, rev int64) {
			canceled = append(canceled, cancellation{session, rev})
		},
	})
	v.applyState(BubbleState{
		ID: 1, Generation: 1, SessionID: "sess-empty", ShowID: 1,
		ReqID: "req-empty", Trigger: "input", Text: "hello",
		SourceLang: "auto", TargetLang: "zh-HANS", Settings: cfg,
	}, true)
	v.applyMeta(RunMetaMsg{ReqID: "req-empty", Services: []string{"google"}})
	v.applyResult(TranslationUpdateMsg{
		ReqID: "req-empty", Service: "google", Text: "old result", DetectedLang: "en", Streaming: true,
	})
	tt := ui.NewTester(v.render, 480, 420)
	if !tt.HasText("old result") || v.detected != "en" {
		t.Fatal("expected an active streaming result with detected language before clearing")
	}
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	if v.composerText != "" {
		t.Fatalf("keyboard clear left composer=%q", v.composerText)
	}
	if len(canceled) != 1 || canceled[0].session != "sess-empty" || canceled[0].rev <= 0 {
		t.Errorf("keyboard clear cancellations = %+v, want one current-session cancellation with a new revision", canceled)
	}
	if v.reqID != "" || len(v.cards) != 0 || v.detected != "" {
		t.Errorf("keyboard clear left reqID=%q cards=%v detected=%q", v.reqID, v.cards, v.detected)
	}
	for range 3 {
		tt.Frame()
	}
	v.applyResult(TranslationUpdateMsg{
		ReqID: "req-empty", Service: "google", Text: "late result", DetectedLang: "en",
	})
	tt.Frame()
	if len(v.cards) != 0 || v.reqID != "" || v.detected != "" || tt.HasText("late result") || tt.HasText("old result") {
		t.Errorf("stale result survived keyboard clear: reqID=%q cards=%v detected=%q texts=%v", v.reqID, v.cards, v.detected, tt.Texts())
	}
	if len(canceled) != 1 || len(runs) != 0 {
		t.Errorf("clear and later frames dispatched cancellations=%+v runs=%+v, want one cancel and no run", canceled, runs)
	}
}

func TestBubbleViewCollapseReportsSettledHeight(t *testing.T) {
	var heights []float64
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		onHeightMeasured: func(h float64) { heights = append(heights, h) },
	})
	v.applyState(BubbleState{
		ID: 1, Generation: 1, SessionID: "sess-collapse", ShowID: 1,
		ReqID: "req-collapse", Trigger: "selection", Text: "hello", Settings: cfg,
	}, false)
	v.applyMeta(RunMetaMsg{ReqID: "req-collapse", Services: []string{"google"}})
	translation := strings.Repeat("translation line\n", 20)
	v.applyResult(TranslationUpdateMsg{ReqID: "req-collapse", Service: "google", Text: translation})
	tt := ui.NewTester(v.render, 480, 760)
	for range 5 {
		tt.Frame()
	}
	if len(heights) == 0 || !tt.HasText(translation) {
		t.Fatalf("expanded result did not settle: heights=%v", heights)
	}
	expanded := heights[len(heights)-1]
	beforeCollapse := len(heights)
	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		tt.Frame()
	}
	collapsed := heights[len(heights)-1]
	if tt.HasText(translation) {
		t.Error("collapsed translation remained visible")
	}
	if len(heights) <= beforeCollapse || collapsed >= expanded {
		t.Errorf("collapse height callbacks = %v, want final height below %v", heights, expanded)
	}
	beforeExpand := len(heights)
	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		tt.Frame()
	}
	if !tt.HasText(translation) {
		t.Error("expanded translation did not return")
	}
	if len(heights) <= beforeExpand || heights[len(heights)-1] != expanded {
		t.Errorf("expand height callbacks = %v, want final height restored to %v", heights, expanded)
	}
}

func TestBubbleViewHeightLimitChangeReportsUnchangedContent(t *testing.T) {
	var heights []float64
	limit := float64(760)
	applied := 240
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		onHeightMeasured: func(h float64) {
			heights = append(heights, h)
			applied = bubbleLayoutHeight(h, limit)
		},
	})
	v.applyState(BubbleState{
		ID: 1, Generation: 1, SessionID: "sess-limit", ShowID: 1,
		ReqID: "req-limit", Trigger: "selection", Text: "hello", Settings: cfg,
	}, false)
	v.applyMeta(RunMetaMsg{ReqID: "req-limit", Services: []string{"google"}})
	translation := strings.Repeat("translation line\n", 80)
	v.applyResult(TranslationUpdateMsg{ReqID: "req-limit", Service: "google", Text: translation})
	tt := ui.NewTester(v.render, 480, 760)
	for range 5 {
		tt.Frame()
	}
	if len(heights) == 0 || heights[len(heights)-1] <= 760 || applied != 760 {
		t.Fatalf("long translation setup: heights=%v applied=%d, want intrinsic above 760 and applied 760", heights, applied)
	}
	// Keep the viewport and content unchanged so only the window's height limit changes.
	for _, nextLimit := range []float64{400, 760} {
		before := len(heights)
		limit = nextLimit
		v.setMaxHeight(limit)
		for range 5 {
			tt.Frame()
		}
		if len(heights) <= before {
			t.Errorf("limit %v did not trigger a height callback: %v", limit, heights)
		}
		if applied != int(limit) {
			t.Errorf("limit %v applied height = %d, want %d", limit, applied, int(limit))
		}
		if !tt.HasText(translation) {
			t.Errorf("limit %v removed the translation", limit)
		}
	}
}

func TestBubbleViewHeightMeasurementSettled(t *testing.T) {
	var heights []float64
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{
		onHeightMeasured: func(h float64) { heights = append(heights, h) },
	})
	v.applyState(BubbleState{
		ID:         1,
		Generation: 1,
		SessionID:  "sess-h",
		ShowID:     1,
		ReqID:      "req-h",
		Trigger:    "selection",
		Text:       strings.Repeat("word ", 20),
		Settings:   cfg,
	}, false)

	tt := ui.NewTester(v.render, 480, 240)
	tt.Frame()
	tt.Frame()
	if len(heights) != 1 {
		t.Fatalf("heights = %v, want exactly 1 settled measurement", heights)
	}
	tt.Frame()
	if len(heights) != 1 {
		t.Fatalf("extra frame re-triggered height measurement: %v", heights)
	}
}

func TestBubbleViewLightDarkThemeFollow(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{
		ID:         1,
		Generation: 1,
		SessionID:  "sess-theme",
		ShowID:     1,
		Trigger:    "input",
		Settings:   cfg,
	}, true)

	tt := ui.NewTester(v.render, 480, 240)
	tt.Frame()
	lightImg := tt.Image()
	gotLight := lightImg.RGBAAt(2, 235)
	if gotLight.R != lightBubblePalette.panel.R || gotLight.G != lightBubblePalette.panel.G || gotLight.B != lightBubblePalette.panel.B {
		t.Fatalf("light panel pixel = %+v, want %+v", gotLight, lightBubblePalette.panel)
	}

	tt.SetDark(true)
	tt.Frame()
	darkImg := tt.Image()
	gotDark := darkImg.RGBAAt(2, 235)
	if gotDark.R != darkBubblePalette.panel.R || gotDark.G != darkBubblePalette.panel.G || gotDark.B != darkBubblePalette.panel.B {
		t.Fatalf("dark panel pixel = %+v, want %+v", gotDark, darkBubblePalette.panel)
	}

	tt.SetDark(false)
	tt.Frame()
	backLight := tt.Image().RGBAAt(2, 235)
	if backLight.R != lightBubblePalette.panel.R || backLight.G != lightBubblePalette.panel.G || backLight.B != lightBubblePalette.panel.B {
		t.Fatalf("reverted light panel pixel = %+v, want %+v", backLight, lightBubblePalette.panel)
	}
}
