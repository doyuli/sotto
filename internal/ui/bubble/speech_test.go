package bubble

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBubbleSpeechMatchesFormerActions(t *testing.T) {
	cfg := testBubbleConfig()
	var spoken [][2]string
	v := newBubbleView(1, cfg, bubbleViewActions{speakText: func(text, lang string, _ func()) { spoken = append(spoken, [2]string{text, lang}) }})
	v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "speech", ShowID: 1, ReqID: "req", Trigger: "selection", Text: " hello ", SourceLang: "en", TargetLang: "zh-HANS", Settings: cfg}, false)
	tt := ui.NewTester(v.render, 480, 700)
	for i := 0; i < 2; i++ {
		if err := tt.Click("朗读输入"); err != nil {
			t.Fatal(err)
		}
	}
	if len(spoken) != 2 || spoken[0] != ([2]string{"hello", "en"}) || spoken[1] != spoken[0] {
		t.Fatalf("repeat source speech=%v", spoken)
	}
	v.applyMeta(RunMetaMsg{ReqID: "req", SourceLang: "ja", TargetLang: "zh-HANS", Services: []string{"google"}})
	v.applyResult(TranslationUpdateMsg{ReqID: "req", Service: "google", Text: "你好", Streaming: true})
	tt.Frame()
	if err := tt.Click("朗读 Google"); err != nil {
		t.Fatal(err)
	}
	if len(spoken) != 2 {
		t.Fatal("streaming card spoke")
	}
	if err := tt.Click("朗读输入"); err != nil {
		t.Fatal(err)
	}
	if spoken[2][1] != "ja" {
		t.Fatalf("detected language ignored: %v", spoken)
	}
	v.applyResult(TranslationUpdateMsg{ReqID: "req", Service: "google", Text: "你好"})
	tt.Frame()
	if err := tt.Click("朗读 Google"); err != nil {
		t.Fatal(err)
	}
	if spoken[3] != ([2]string{"你好", "zh-HANS"}) {
		t.Fatalf("target speech=%v", spoken)
	}
	v.applyResult(TranslationUpdateMsg{ReqID: "req", Service: "google", Text: "未完成", Err: "中断"})
	tt.Frame()
	if err := tt.Click("朗读 Google"); err != nil {
		t.Fatal(err)
	}
	if len(spoken) != 4 {
		t.Fatal("failed card spoke")
	}
}

func TestBubbleSpeechErrorsIgnoreOldPresentation(t *testing.T) {
	var failures []func()
	v := newBubbleView(1, testBubbleConfig(), bubbleViewActions{speakText: func(_, _ string, fail func()) { failures = append(failures, fail) }})
	v.sessionID = "first"
	v.showID = 1
	v.speak("one", "en")
	v.speak("two", "en")
	failures[0]()
	if v.speechError {
		t.Fatal("obsolete attempt showed error")
	}
	failures[1]()
	if !v.speechError {
		t.Fatal("current failure was lost")
	}
	tt := ui.NewTester(v.render, 480, 420)
	if !tt.HasText("朗读不可用") {
		t.Fatal("missing error toast")
	}
	v.speechError = false
	v.showID++
	failures[1]()
	if v.speechError {
		t.Fatal("old presentation showed error")
	}
	v.speak("   ", "en")
	if len(failures) != 2 {
		t.Fatal("empty text dispatched")
	}
}
