package bubble

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBubbleAutomaticTargetAcrossSubmissions(t *testing.T) {
	// Resolved metadata used to turn the next automatic submission into a fixed target.
	cfg := testBubbleConfig()
	cfg.TargetLanguage = "auto"
	var req RunReq
	var spokenLang string
	v := newBubbleView(1, cfg, bubbleViewActions{
		run:       func(r RunReq) { req = r },
		speakText: func(_, lang string, _ func()) { spokenLang = lang },
	})
	v.applyState(BubbleState{SessionID: "auto", Trigger: "input", Settings: cfg}, true)
	tt := ui.NewTester(v.render, 480, 700)
	tt.Type("早安")
	tt.Key(0, ui.KeyEnter)
	if req.TargetLang != "auto" || req.Text != "早安" {
		t.Fatalf("first submission = %+v", req)
	}
	v.applyMeta(RunMetaMsg{ReqID: req.ReqID, TargetLang: "en", Services: cfg.Services.Enabled})
	v.applyResult(TranslationUpdateMsg{ReqID: req.ReqID, Service: "google", Text: "Good morning", DetectedLang: "zh"})
	v.applyResult(TranslationUpdateMsg{ReqID: req.ReqID, Service: "baidu", Err: "超时"})
	tt.Frame()
	if err := tt.Click("朗读 Google"); err != nil || spokenLang != "en" || !tt.HasText("aB") {
		t.Fatalf("English result actions: speech=%q err=%v", spokenLang, err)
	}
	if err := tt.Click("重试"); err != nil || req.Service != "baidu" || req.TargetLang != "en" {
		t.Fatalf("card retry = %+v, err=%v", req, err)
	}
	v.applyMeta(RunMetaMsg{ReqID: req.ReqID, TargetLang: "en", Services: cfg.Services.Enabled})
	if err := tt.Click("要翻译的原文"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Good morning")
	tt.Key(0, ui.KeyEnter)
	if req.Text != "Good morning" || req.TargetLang != "auto" || req.Service != "" {
		t.Fatalf("next full submission = %+v", req)
	}
	v.applyMeta(RunMetaMsg{ReqID: req.ReqID, TargetLang: "zh-HANS", Services: cfg.Services.Enabled})
	v.applyResult(TranslationUpdateMsg{ReqID: req.ReqID, Service: "google", Text: "早安", DetectedLang: "en"})
	tt.Frame()
	if err := tt.Click("交换语言"); err != nil || req.SourceLang != "zh-HANS" || req.TargetLang != "en" {
		t.Fatalf("swapped direction = %+v, err=%v", req, err)
	}
}

func TestBubbleCanLockResolvedTarget(t *testing.T) {
	// Selecting the displayed translation language must also leave automatic mode.
	cfg := testBubbleConfig()
	cfg.TargetLanguage = "auto"
	var runs []RunReq
	v := newBubbleView(1, cfg, bubbleViewActions{run: func(r RunReq) { runs = append(runs, r) }})
	v.applyState(BubbleState{SessionID: "lock", ReqID: "first", Trigger: "selection", Text: "早安", Settings: cfg}, false)
	v.applyMeta(RunMetaMsg{ReqID: "first", TargetLang: "en", Services: cfg.Services.Enabled})
	tt := ui.NewTester(v.render, 480, 700)
	for _, label := range []string{"目标语言", "英语", "要翻译的原文"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Good morning")
	tt.Key(0, ui.KeyEnter)
	if len(runs) != 2 || runs[0].TargetLang != "en" || runs[1].Text != "Good morning" || runs[1].TargetLang != "en" {
		t.Fatalf("manual target submissions = %+v", runs)
	}
	for _, label := range []string{"目标语言", "自动互译"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if len(runs) != 3 || runs[2].TargetLang != "auto" {
		t.Fatalf("restore automatic target = %+v", runs)
	}
}

func TestBubbleCardWholeHeaderTogglesOnce(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "header", ShowID: 1,
		ReqID: "header-req", Trigger: "selection", Text: "hello", Settings: cfg}, false)
	v.applyMeta(RunMetaMsg{ReqID: "header-req", Services: []string{"google"}})
	v.applyResult(TranslationUpdateMsg{ReqID: "header-req", Service: "google", Text: "你好"})
	tt := ui.NewTester(v.render, 480, 560)
	for _, fraction := range []float32{0.06, 0.5, 0.96} {
		for _, wantCollapsed := range []bool{true, false} {
			header, ok := tt.Find("折叠 Google")
			if !ok {
				t.Fatal("card header missing")
			}
			tt.ClickAt(header.X+header.W*fraction, header.Y+header.H/2)
			if v.collapsed["google"] != wantCollapsed || tt.HasText("你好") == wantCollapsed {
				t.Fatalf("header position %v collapsed=%v bodyVisible=%v, want collapsed=%v", fraction,
					v.collapsed["google"], tt.HasText("你好"), wantCollapsed)
			}
		}
	}
	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeySpace)
	if v.collapsed["google"] {
		t.Fatal("Space did not expand the focused card header")
	}
}

func TestBubbleComposerMinimumTwoLines(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "two-lines", ShowID: 1,
		Trigger: "input", Settings: cfg}, true)
	tt := ui.NewTester(v.render, 480, 360)
	empty, ok := tt.Find("要翻译的原文")
	if !ok || empty.H < 2*14*1.55 {
		t.Fatalf("empty composer bounds=%+v found=%v, want at least two text lines", empty, ok)
	}
	tt.Type("one")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("two")
	two, ok := tt.Find("要翻译的原文")
	if !ok || two.H != empty.H || v.composerScroll.MaxY > 0 {
		t.Fatalf("two lines changed minimum height or overflowed: empty=%+v two=%+v maxY=%v", empty, two, v.composerScroll.MaxY)
	}
}

func TestBubbleLanguageSelectWidthKeyboardAndDismissal(t *testing.T) {
	for _, height := range []int{240, 420} {
		cfg := testBubbleConfig()
		var runs []RunReq
		var closes int
		v := newBubbleView(1, cfg, bubbleViewActions{
			run:   func(req RunReq) { runs = append(runs, req) },
			close: func(string, int64, int64) bool { closes++; return true },
		})
		v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "language", ShowID: 1,
			Trigger: "selection", Text: "hello", Settings: cfg}, false)
		tt := ui.NewTester(v.render, 480, height)
		for _, label := range []string{"源语言", "目标语言"} {
			trigger, ok := tt.Find(label)
			if !ok {
				t.Fatalf("%s missing", label)
			}
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			panel, ok := tt.Find(label + "选项")
			if !ok || panel.X != trigger.X || panel.W != trigger.W || panel.Y < 0 || panel.Y+panel.H > float32(height) {
				t.Fatalf("height=%d %s trigger=%+v popup=%+v found=%v", height, label, trigger, panel, ok)
			}
			tt.Key(0, ui.KeyEscape)
			if _, ok := tt.Find(label + "选项"); ok || closes != 0 {
				t.Fatal("Escape must dismiss only the language popup")
			}
		}
		if err := tt.Click("目标语言"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyEnd)
		tt.Key(0, ui.KeyEnter)
		if v.targetLang != "it" || len(runs) != 1 || runs[0].TargetLang != "it" {
			t.Fatalf("keyboard selected target=%q runs=%+v", v.targetLang, runs)
		}
	}
}
