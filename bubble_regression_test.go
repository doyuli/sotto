package main

import (
	"errors"
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
	// Incremental results and same-presentation snapshots preserve user choice.
	v.applyResult(TranslationUpdateMsg{ReqID: "first", Service: "baidu", Text: "另一译文"})
	tt.Frame()
	if !v.collapsed["google"] {
		t.Fatal("incremental result expanded a collapsed card")
	}
	state.Results = []TranslationUpdateMsg{{ReqID: "first", Service: "google", Text: "译文"}}
	v.applyState(state, false)
	tt.Frame()
	if !v.collapsed["google"] {
		t.Fatal("same presentation reset collapse state")
	}
	state.Generation++
	state.SessionID, state.ReqID = "second", "second"
	state.ShowID++
	state.Results = []TranslationUpdateMsg{{ReqID: "second", Service: "google", Text: "新译文"}}
	v.applyState(state, false)
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
			v.startRun("hello", service)
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

func TestBubbleClipboardCompletionOwnership(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprintf("discard=%t", discard), func(t *testing.T) {
			cfg := testBubbleConfig()
			var done func(error)
			calls := 0
			v := newBubbleView(1, cfg, bubbleViewActions{
				restoreClipboard: func(_ string, complete func(error)) { calls++; done = complete },
				discardClipboard: func(_ string, complete func()) { calls++; done = func(error) { complete() } },
			})
			state := BubbleState{ID: 1, Generation: 1, SessionID: "clipboard", ShowID: 1,
				ClipboardRecoveryID: "recovery", Settings: cfg}
			v.applyState(state, false)
			tt := ui.NewTester(v.render, 480, 420)
			label := "重试恢复剪贴板"
			if discard {
				label = "放弃恢复，继续取词"
			}
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			if !v.clipboardBusy || !tt.HasText("正在处理剪贴板…") {
				t.Fatal("pending operation not shown")
			}
			v.resolveClipboard(discard)
			if calls != 1 {
				t.Fatal("duplicate operation dispatched")
			}
			// UI remains editable while OS work is pending.
			if err := tt.Click("要翻译的原文"); err != nil {
				t.Fatal(err)
			}
			tt.Type("editable")
			if v.composerText != "editable" {
				t.Fatal("pending operation blocked editing")
			}
			if !discard {
				done(errors.New("暂时无法恢复"))
				tt.Frame()
				if v.clipboardBusy || v.clipboardRecoveryID == "" || !tt.HasText("暂时无法恢复") {
					t.Fatal("failure did not allow retry")
				}
				v.resolveClipboard(false)
			}
			oldDone := done
			state.Generation++
			state.SessionID, state.ClipboardRecoveryID = "next", "new-recovery"
			state.ShowID++
			v.applyState(state, false)
			v.resolveClipboard(discard)
			oldDone(nil)
			if !v.clipboardBusy || v.clipboardRecoveryID != "new-recovery" || v.clipboardToast != "" {
				t.Fatal("old callback changed new presentation")
			}
			done(nil)
			tt.Frame()
			if v.clipboardBusy || v.clipboardRecoveryID != "" {
				t.Fatal("current operation did not finish")
			}
			// A hidden presentation cannot surface a late error toast.
			state.Generation++
			state.SessionID, state.ClipboardRecoveryID = "hidden", "hidden-recovery"
			v.applyState(state, false)
			v.resolveClipboard(discard)
			v.onStopped(RunStoppedMsg{SessionID: "hidden", ShowID: state.ShowID, Revision: 1})
			done(errors.New("late error"))
			if v.clipboardToast != "" {
				t.Fatal("hidden presentation showed late toast")
			}
		})
	}
}

func TestBubbleClipboardRefocusPreservesPendingOperation(t *testing.T) {
	for _, outcome := range []string{"restore", "discard", "failure", "completed-before-notice"} {
		t.Run(outcome, func(t *testing.T) {
			cfg := testBubbleConfig()
			var done func(error)
			calls := 0
			v := newBubbleView(1, cfg, bubbleViewActions{
				restoreClipboard: func(_ string, complete func(error)) { calls++; done = complete },
				discardClipboard: func(_ string, complete func()) { calls++; done = func(error) { complete() } },
			})
			v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "refocus", ShowID: 1,
				ClipboardRecoveryID: "recovery", Settings: cfg}, false)
			tt := ui.NewTester(v.render, 480, 420)
			discard := outcome == "discard"
			v.resolveClipboard(discard)
			// Global recovery may already be gone while Window.Update is queued.
			id := "recovery"
			if outcome == "completed-before-notice" {
				id = ""
			}
			for show := int64(2); show <= 4; show++ {
				v.onPresented(BubblePresentedMsg{SessionID: "refocus", ShowID: show, ClipboardRecoveryID: id, Focus: true})
				tt.Frame()
				v.resolveClipboard(discard)
				if !v.clipboardBusy || v.clipboardRecoveryID != "recovery" || calls != 1 || !tt.HasText("正在处理剪贴板…") {
					t.Fatal("refocus revoked or duplicated the pending operation")
				}
			}
			var err error
			if outcome == "failure" {
				err = errors.New("恢复失败，请重试")
			}
			done(err)
			tt.Frame()
			if v.clipboardBusy {
				t.Fatal("completion after refocus left operation busy")
			}
			if err != nil {
				if v.clipboardRecoveryID != "recovery" || !tt.HasText(err.Error()) {
					t.Fatal("refocus lost recovery error or retry identity")
				}
			} else if v.clipboardRecoveryID != "" || tt.HasText("之前复制的内容尚未恢复，可重试或放弃后继续取词。") {
				t.Fatal("successful completion after refocus retained recovery banner")
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
	v.startRun("", "")
	v.applyState(state, false)
	if v.stateRevision != 3 || v.reqID != "" || v.submittedText != "" || v.runServices != nil {
		t.Fatalf("old state revived cleared run: %+v", v)
	}
}
