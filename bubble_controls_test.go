package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBubbleCardWholeHeaderTogglesOnce(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "header", ShowID: 1,
		LayoutID: 1, ReqID: "header-req", Trigger: "selection", Text: "hello", Settings: cfg}, false)
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
		LayoutID: 1, Trigger: "input", Settings: cfg}, true)
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
			LayoutID: 1, Trigger: "selection", Text: "hello", Settings: cfg}, false)
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
