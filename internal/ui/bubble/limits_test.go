package bubble

import (
	"github.com/egoist/mygo/ui"
	"sotto/internal/translate"
	"strings"
	"testing"
)

func TestComposerRejectsOversizedSubmissionAndRecovers(t *testing.T) {
	// Enter used to fan an entire pasted document out to every enabled service.
	cfg := testBubbleConfig()
	var runs []RunReq
	v := newBubbleView(1, cfg, bubbleViewActions{run: func(r RunReq) { runs = append(runs, r) }})
	text := strings.Repeat("x", translate.MaxInputRunes+1)
	v.applyState(BubbleState{ID: 1, Generation: 1, SessionID: "budget", ShowID: 1, Trigger: "input", Text: text, Settings: cfg}, true)
	tt := ui.NewTester(v.render, 480, 420)
	tt.Key(0, ui.KeyEnter)
	if len(runs) != 0 || v.composerText != text || !strings.Contains(v.message, "18000") {
		t.Fatal("oversized submission was sent or silently truncated")
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	tt.Type("short")
	tt.Key(0, ui.KeyEnter)
	if len(runs) != 1 || runs[0].Text != "short" {
		t.Fatal("valid submission did not recover")
	}
}
