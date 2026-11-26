package bubble

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBubbleNewPresentationExpandsCards(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	state := BubbleState{ID: 1, Generation: 1, SessionID: "first", ShowID: 1, Revision: 0,
		ReqID: "first", Text: "hello", Settings: cfg}
	v.applyState(state, false)
	v.applyResult(TranslationUpdateMsg{ReqID: "first", Service: "google", Text: "译文"})
	tt := ui.NewTester(v.render, 480, 600)
	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatal(err)
	}
	// Incremental results and same-presentation notifications preserve user choice.
	v.applyResult(TranslationUpdateMsg{ReqID: "first", Service: "baidu", Text: "另一译文"})
	tt.Frame()
	if !v.collapsed["google"] {
		t.Fatal("incremental result expanded a collapsed card")
	}
	v.onPresented(BubblePresentedMsg{SessionID: "first", ShowID: state.ShowID, Revision: state.Revision})
	tt.Frame()
	if !v.collapsed["google"] {
		t.Fatal("same presentation reset collapse state")
	}
	state.Generation++
	state.SessionID, state.ReqID = "second", "second"
	state.ShowID++
	v.applyState(state, false)
	v.applyResult(TranslationUpdateMsg{ReqID: "second", Service: "google", Text: "新译文"})
	tt.Frame()
	if v.collapsed["google"] || !tt.HasText("新译文") {
		t.Fatal("reopened panel retained collapsed cards")
	}
	if err := tt.Click("折叠 Google"); err != nil {
		t.Fatal(err)
	}
	v.onPresented(BubblePresentedMsg{SessionID: "second", ShowID: state.ShowID + 1})
	tt.Frame()
	if v.collapsed["google"] || !tt.HasText("新译文") {
		t.Fatal("refocused presentation retained collapsed cards")
	}
}

func TestBubbleFullRunResetsScrollButRetryPreservesIt(t *testing.T) {
	for _, service := range []string{"", "baidu"} {
		t.Run("service="+service, func(t *testing.T) {
			cfg := testBubbleConfig()
			v := newBubbleView(1, cfg, bubbleViewActions{})
			v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "scroll", ShowID: 1,
				ReqID: "old", Text: "hello", Settings: cfg}, false)
			v.applyResult(TranslationUpdateMsg{ReqID: "old", Service: "google", Text: strings.Repeat("result\n", 40)})
			v.applyResult(TranslationUpdateMsg{ReqID: "old", Service: "baidu", Err: "网络连接失败"})
			tt := ui.NewTester(v.render, 480, 240)
			tt.Scroll(470, 210, 0, 120)
			before := v.scroll.Y
			if before <= 0 {
				t.Fatal("fixture did not scroll")
			}
			v.StartRun("hello", service)
			tt.Frame()
			if service == "" && v.scroll.Y != 0 {
				t.Errorf("full run retained scroll=%v", v.scroll.Y)
			}
			if service != "" && v.scroll.Y != before {
				t.Errorf("retry moved scroll from %v to %v", before, v.scroll.Y)
			}
		})
	}
}

func TestBubbleComposerRespectsWorkAreaBudget(t *testing.T) {
	for _, limit := range []int{300, 400, 760} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			cfg := testBubbleConfig()
			v := newBubbleView(1, cfg, bubbleViewActions{})
			v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "short", ShowID: 1,
				MaxHeight: float64(limit), Text: strings.Repeat("line\n", 20), Settings: cfg}, false)
			tt := ui.NewTester(v.render, 480, limit)
			r := bubbleScrollInputRect(t, tt)
			budget := float32(min(112, float64(limit)*0.18))
			if r.H > budget+0.1 || r.H <= 0 || v.composerScroll.MaxY <= 0 {
				t.Fatalf("composer=%+v budget=%v scroll=%+v", r, budget, v.composerScroll)
			}
			v.setMaxHeight(300)
			tt.Frame()
			if r := bubbleScrollInputRect(t, tt); r.H > 54.1 {
				t.Errorf("changed work area did not constrain composer: %+v", r)
			}
		})
	}
}

func TestBubbleClearRejectsOldState(t *testing.T) {
	cfg := testBubbleConfig()
	v := newBubbleView(1, cfg, bubbleViewActions{})
	state := BubbleState{ID: 1, Generation: 1, SessionID: "clear", ShowID: 1, Revision: 2,
		ReqID: "old", Text: "hello", Settings: cfg}
	v.applyState(state, false)
	v.runServices = map[string]bool{"google": true}
	v.StartRun("", "")
	v.applyState(state, false)
	if v.stateRevision != 3 || v.reqID != "" || v.submittedText != "" || v.runServices != nil {
		t.Fatalf("old state revived cleared run: %+v", v)
	}
}
