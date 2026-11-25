package bubble

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/selection"
)

func newDismissTestBubble(t *testing.T, trigger, phase string) (*Manager, *bubble) {
	t.Helper()
	m := NewManager(nil, Actions{})
	b := &bubble{id: 1, win: &mygo.Window{}, ready: true}
	m.byID[b.id] = b
	b.replaceContent(config.Default(), trigger, "original source", phase, "", nil)
	b.present()
	b.canShow = func() bool { return true }
	t.Cleanup(b.stopWork)
	return m, b
}

func dismissTestBubble(m *Manager, b *bubble) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return b.dismiss()
}

func startDismissTestJobs(t *testing.T, m *Manager, b *bubble, view *bubbleView) (context.Context, context.Context) {
	t.Helper()
	fullMeta := RunMetaMsg{
		ReqID: "full", Services: []string{"openai", "google", "baidu"},
		SourceLang: "en", TargetLang: "zh-HANS",
	}
	full, ok := m.StartRun(b.win, RunReq{
		SessionID: b.sessionID, ReqID: "full", Revision: 0,
		Text: b.text, SourceLang: "en", TargetLang: "zh-HANS",
	})
	if !ok || !m.PushMeta(b.win, fullMeta, "") {
		t.Fatal("could not start full translation")
	}
	if view != nil {
		view.reqID, view.revision, view.stateRevision = "full", 0, 0
		view.applyMeta(fullMeta)
	}
	retryMeta := RunMetaMsg{
		ReqID: "retry", Services: []string{"baidu"}, SourceLang: "en", TargetLang: "zh-HANS",
	}
	retry, ok := m.StartRun(b.win, RunReq{
		SessionID: b.sessionID, ReqID: "retry", Revision: 1, Service: "baidu",
		Text: b.text, SourceLang: "en", TargetLang: "zh-HANS",
	})
	if !ok || !m.PushMeta(b.win, retryMeta, "baidu") {
		t.Fatal("could not start concurrent service retry")
	}
	if view != nil {
		view.reqID, view.revision, view.stateRevision = "retry", 1, 1
		view.retryService = "baidu"
		view.serviceReqIDs["baidu"] = "retry"
		view.applyMeta(retryMeta)
	}
	for _, result := range []TranslationUpdateMsg{
		{ReqID: "full", Service: "openai", Text: "partial", Streaming: true},
		{ReqID: "full", Service: "openai", Text: "partial translation", Streaming: true},
		{ReqID: "full", Service: "google", Text: "finished translation"},
		{ReqID: "retry", Service: "baidu", Text: "partial retry", Streaming: true},
	} {
		if !m.PushResult(b.win, result) {
			t.Fatalf("could not record %s result", result.Service)
		}
		if view != nil {
			view.applyResult(result)
		}
	}
	return full, retry
}

func TestBubbleDismissCancelsAllJobsAndSettlesView(t *testing.T) {
	for _, trigger := range []string{"input", "selection", "ocr"} {
		t.Run(trigger, func(t *testing.T) {
			m, b := newDismissTestBubble(t, trigger, "")
			// Results belong to the view; dismissal must settle its cards without
			// reconstructing them from a second backend result store.
			view := newBubbleView(b.id, config.Default(), bubbleViewActions{})
			view.applyState(m.state(b, config.Default()), false)
			full, retry := startDismissTestJobs(t, m, b, view)
			// Attach an independently pending acquisition after StartRun, which
			// intentionally cancels capture when a full translation takes over.
			capture, cancelCapture := context.WithCancel(context.Background())
			b.cancelCapture = cancelCapture
			before := m.state(b, config.Default())
			if !dismissTestBubble(m, b) {
				t.Fatal("current unpinned presentation rejected dismissal")
			}
			if full.Err() != context.Canceled || retry.Err() != context.Canceled || capture.Err() != context.Canceled {
				t.Fatal("dismissal left full translation, service retry, or capture running")
			}
			view.onStopped(RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision})
			// Already queued UI callbacks must not revive the stopped cards.
			view.applyMeta(RunMetaMsg{ReqID: "full", Services: []string{"openai", "google"}})
			view.applyResult(TranslationUpdateMsg{ReqID: "full", Service: "openai", Text: "late full", Streaming: true})
			view.applyResult(TranslationUpdateMsg{ReqID: "retry", Service: "baidu", Text: "late retry", Streaming: true})
			state := m.state(b, config.Default())
			if state.ReqID != "" || len(b.serviceRequests) != 0 || len(b.runCancels) != 0 || b.cancelCapture != nil || view.reqID != "" || len(view.serviceReqIDs) != 0 {
				t.Fatalf("dismissal retained active request ownership: %+v", state)
			}
			if !b.closeClaimed || b.canShow == nil || b.canShow() || state.Revision != before.Revision+1 || b.fullRevision != state.Revision || b.fullReqID != "" {
				t.Fatal("dismissal did not revoke presentation and full-run ownership exactly once")
			}
			if state.SessionID != before.SessionID || state.ShowID != before.ShowID || state.Generation != before.Generation || state.Text != before.Text {
				t.Fatal("dismissal replaced content instead of retaining the stopped snapshot")
			}
			if len(view.cards) != 3 {
				t.Fatalf("dismissal lost existing cards: %+v", view.cards)
			}
			for service, text := range map[string]string{"openai": "partial translation", "baidu": "partial retry"} {
				result := view.cards[service]
				if result == nil || result.Text != text || result.Loading || result.Streaming || result.Err == "" {
					t.Errorf("dismissed stream must retain partial text and settle: %+v", result)
				}
			}
			if result := view.cards["google"]; result == nil || result.Text != "finished translation" || result.Loading || result.Streaming || result.Err != "" {
				t.Errorf("dismissal changed a finished card: %+v", result)
			}
		})
	}
}

func TestBubbleDismissRecognitionRejectsLateCaptureAndShow(t *testing.T) {
	for _, trigger := range []string{"selection", "ocr"} {
		t.Run(trigger, func(t *testing.T) {
			m, b := newDismissTestBubble(t, trigger, "recognizing")
			capture, cancel := context.WithCancel(context.Background())
			b.cancelCapture = cancel
			shown := Shown{Win: b.win, ReqID: b.sessionID, Text: b.text}
			// Native readiness cannot release first show after dismissal.
			if !dismissTestBubble(m, b) || capture.Err() != context.Canceled {
				t.Fatal("recognition dismissal did not cancel capture")
			}
			before := m.state(b, config.Default())
			if before.Phase != "error" || before.Message == "" {
				t.Fatalf("dismissed recognition still appears pending: %+v", before)
			}
			for _, message := range []string{"", "late recognition error"} {
				if m.CompleteCapture(config.Default(), shown, "late recognized text", message) {
					t.Fatal("dismissed capture completed into the stopped session")
				}
			}
			if b.claimShow() {
				t.Fatal("late show revived the dismissed recognition")
			}
			if after := m.state(b, config.Default()); !reflect.DeepEqual(before, after) {
				t.Fatalf("late capture or show mutated the stopped snapshot: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestBubbleDismissedCaptureRequiresOpenPresentation(t *testing.T) {
	m, b := newDismissTestBubble(t, "ocr", "recognizing")
	shown := Shown{Win: b.win, ReqID: b.sessionID, Text: b.text}
	// Isolate the closure guard: revision -1 still satisfies capture's revision
	// check. A close claim must independently prevent committing recognition.
	b.closeClaimed = true
	before := m.state(b, config.Default())
	if m.CompleteCapture(config.Default(), shown, "late recognized text", "") {
		t.Fatal("capture ignored closure while its recognition revision still matched")
	}
	if after := m.state(b, config.Default()); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected capture changed the closed presentation")
	}
}

func TestBubbleDismissRejectsLateWorkAtAnyRevision(t *testing.T) {
	m, b := newDismissTestBubble(t, "input", "")
	startDismissTestJobs(t, m, b, nil)
	if !dismissTestBubble(m, b) {
		t.Fatal("dismissal rejected")
	}
	before := m.state(b, config.Default())
	for _, attempt := range []struct{ request, service string }{{"full", ""}, {"retry", "baidu"}} {
		if m.PushMeta(b.win, RunMetaMsg{ReqID: attempt.request, Services: []string{"google", "baidu"}}, attempt.service) {
			t.Errorf("accepted late metadata for %s", attempt.request)
		}
		for _, service := range []string{"openai", "google", "baidu"} {
			if m.PushResult(b.win, TranslationUpdateMsg{ReqID: attempt.request, Service: service, Text: "late", Streaming: true}) {
				t.Errorf("accepted late %s result for %s", service, attempt.request)
			}
		}
		for _, revision := range []int64{before.Revision - 1, before.Revision, before.Revision + 1, 1 << 60} {
			ctx, ok := m.StartRun(b.win, RunReq{
				SessionID: b.sessionID, ReqID: "late request", Revision: revision,
				Service: attempt.service, Text: "late source", SourceLang: "ja", TargetLang: "en",
			})
			if ok || ctx != nil {
				t.Fatalf("dismissed presentation accepted service %q at revision %d", attempt.service, revision)
			}
		}
		m.FinishRun(b.win, attempt.request)
	}
	if b.claimShow() {
		t.Fatal("late show reopened the dismissed bubble")
	}
	if after := m.state(b, config.Default()); !reflect.DeepEqual(before, after) || len(b.runCancels) != 0 {
		t.Fatalf("late work changed the stopped snapshot: before=%+v after=%+v", before, after)
	}
}

func TestBubbleDismissIsIdempotentAndPinnedDismissIsNoOp(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "repeated", true: "pinned"}[pinned], func(t *testing.T) {
			m, b := newDismissTestBubble(t, "selection", "")
			full, retry := startDismissTestJobs(t, m, b, nil)
			b.pinned = pinned
			calls := make(map[string]int)
			for request, cancel := range b.runCancels {
				request, cancel := request, cancel
				b.runCancels[request] = func() { calls[request]++; cancel() }
			}
			capture, cancel := context.WithCancel(context.Background())
			b.cancelCapture = func() { calls["capture"]++; cancel() }
			if !pinned && !dismissTestBubble(m, b) {
				t.Fatal("first dismissal rejected")
			}
			before := m.state(b, config.Default())
			for range 3 {
				if dismissTestBubble(m, b) {
					t.Fatal("pinned or already dismissed presentation accepted dismissal")
				}
			}
			if after := m.state(b, config.Default()); !reflect.DeepEqual(before, after) {
				t.Fatalf("no-op dismissal mutated the snapshot: before=%+v after=%+v", before, after)
			}
			wantCalls := 1
			if pinned {
				wantCalls = 0
				if full.Err() != nil || retry.Err() != nil || capture.Err() != nil || b.closeClaimed || !b.canShow() || len(b.runCancels) != 2 || b.cancelCapture == nil || b.fullReqID != "full" {
					t.Fatal("pinned dismissal changed live jobs or presentation ownership")
				}
			}
			for _, job := range []string{"full", "retry", "capture"} {
				if calls[job] != wantCalls {
					t.Errorf("%s cancellation called %d times, want %d", job, calls[job], wantCalls)
				}
			}
		})
	}
}

func TestBubbleDismissAllowsReplacementButNotOldSession(t *testing.T) {
	for _, trigger := range []string{"input", "selection", "ocr"} {
		t.Run(trigger, func(t *testing.T) {
			m, b := newDismissTestBubble(t, trigger, "")
			startDismissTestJobs(t, m, b, nil)
			oldSession, oldShow, oldGeneration := b.sessionID, b.showID, b.generation
			if !dismissTestBubble(m, b) {
				t.Fatal("dismissal rejected")
			}
			b.replaceContent(config.Default(), trigger, "replacement source", "", "", nil)
			b.present()
			b.canShow = func() bool { return true }
			if b.closeClaimed || b.sessionID == oldSession || b.showID <= oldShow || b.generation <= oldGeneration || b.presentationShown {
				t.Fatal("replacement did not acquire a fresh presentation")
			}
			for _, service := range []string{"", "google"} {
				if _, ok := m.StartRun(b.win, RunReq{SessionID: oldSession, ReqID: "old late", Revision: 1 << 60, Service: service}); ok {
					t.Fatal("old session entered the replacement")
				}
			}
			if b.claimClose(oldSession, oldShow, 1<<60) || m.CompleteCapture(config.Default(), Shown{Win: b.win, ReqID: oldSession}, "old capture", "") {
				t.Fatal("old presentation callback affected replacement")
			}
			full, retry := startDismissTestJobs(t, m, b, nil)
			if full.Err() != nil || retry.Err() != nil || b.text != "replacement source" {
				t.Fatal("replacement failed to start fresh full and retry jobs")
			}
			if !b.claimShow() || b.claimShow() {
				t.Fatal("replacement did not show exactly once using native readiness")
			}
		})
	}
}

func TestBubblePresentationAndCaptureCompletionDoNotDismissJobs(t *testing.T) {
	m, b := newDismissTestBubble(t, "ocr", "recognizing")
	capture, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.cancelCapture = cancel
	if !b.claimShow() || capture.Err() != nil {
		t.Fatal("first presentation dismissed pending capture")
	}
	if !m.CompleteCapture(config.Default(), Shown{Win: b.win, ReqID: b.sessionID}, "recognized text", "") {
		t.Fatal("current capture completion rejected")
	}
	if capture.Err() != nil || b.closeClaimed || !b.canShow() || b.revision != -1 || !b.presentationShown || b.cancelCapture != nil {
		t.Fatal("capture completion dismissed presentation or retained pending acquisition")
	}
	full, retry := startDismissTestJobs(t, m, b, nil)
	if b.claimShow() || full.Err() != nil || retry.Err() != nil || b.closeClaimed || !b.canShow() {
		t.Fatal("translation repeated first show or dismissed active jobs")
	}
	// Native Hide during reuse is covered by desktop e2e; a zero Window
	// cannot establish callback behavior.
}

// Hiding a stale workspace must also revoke the independent translation jobs.
func TestWorkspaceDismissStopsTransientJobs(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		m, b := newDismissTestBubble(t, "selection", "")
		full, retry := startDismissTestJobs(t, m, b, nil)
		b.pinned = pinned
		if b.stopStalePresentation(0) {
			t.Fatal("current entry was hidden")
		}
		if !b.stopStalePresentation(1) {
			t.Fatal("stale presentation was retained")
		}
		if pinned {
			if full.Err() != nil || retry.Err() != nil || b.closeClaimed {
				t.Fatal("fixed work was canceled")
			}
		} else {
			if full.Err() == nil || retry.Err() == nil {
				t.Fatal("stale work continued")
			}
			if m.PushResult(b.win, TranslationUpdateMsg{ReqID: "full", Service: "openai", Text: "late"}) || m.PushMeta(b.win, RunMetaMsg{ReqID: "full"}, "") {
				t.Fatal("accepted late work")
			}
			if b.stopStalePresentation(1) || b.claimShow() {
				t.Fatal("dismissal repeated or reopened window")
			}
		}
	}
}

// Workspace restoration must not let an old input consume a new panel entry
// while an unrelated selection is still pending.
func TestPinnedRestorePreservesEntryOwnership(t *testing.T) {
	m, b := newDismissTestBubble(t, "input", "")
	var gate testGate
	old := gate.Begin()
	b.canShow = old.Valid
	b.pinned = true
	b.claimShow()
	full, retry := startDismissTestJobs(t, m, b, nil)
	pending := gate.Begin()
	defer pending.Cancel()
	if !b.stopStalePresentation(1) || !m.claimPinnedRestore(b, 1) {
		t.Fatal("fixed bubble could not be restored")
	}
	if b.stopStalePresentation(1) || b.canShow() || full.Err() != nil || retry.Err() != nil {
		t.Fatal("restore changed entry ownership or independent translation jobs")
	}
	if got, _ := m.changePin(config.Default(), b.win, b.id, b.sessionID, b.showID, false); got != b {
		t.Fatal("restored bubble could not be unpinned")
	}
	// Reject before querying the zero Window: the panel action must acquire
	// a fresh entry, which will revoke the pending selection.
	if m.FocusVisibleInput() || m.HideVisibleInput() || !pending.Valid() {
		t.Fatal("old input bypassed entry acquisition")
	}
	fresh := gate.Begin()
	defer fresh.Cancel()
	if pending.Valid() || b.canShow() || !fresh.Valid() {
		t.Fatal("new panel entry failed to reject old acquisition")
	}
	if !b.stopStalePresentation(2) || full.Err() == nil || retry.Err() == nil {
		t.Fatal("unpinned restoration survived a later workspace change")
	}
}

func TestPinnedRestoreRejectsObsoleteMenuActions(t *testing.T) {
	for _, state := range []string{"unpinned", "closing", "closed", "replaced"} {
		t.Run(state, func(t *testing.T) {
			m, b := newDismissTestBubble(t, "input", "")
			b.pinned = true
			switch state {
			case "unpinned":
				b.pinned = false
			case "closing":
				b.closeClaimed = true
			case "closed":
				delete(m.byID, b.id)
			case "replaced":
				m.byID[b.id] = &bubble{id: b.id, pinned: true}
			}
			if m.claimPinnedRestore(b, 1) || b.workspace != 0 {
				t.Fatal("obsolete menu action renewed presentation")
			}
		})
	}
}

func TestCommitTransientBubbleHideOnlyNotifiesInputAutoHide(t *testing.T) {
	// Keyboard blur must not mark the next tray click as a mouse dismissal.
	for _, tc := range []struct {
		name         string
		trigger      string
		primaryPress bool
		wantCalls    int
	}{
		{"input left-click dismissal", "input", true, 1},
		{"input keyboard or right-click dismissal", "input", false, 0},
		{"selection left-click dismissal", "selection", true, 0},
		{"ocr left-click dismissal", "ocr", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, b := newDismissTestBubble(t, tc.trigger, "")
			calls, hidden := 0, 0
			var press selection.PrimaryPress
			if tc.primaryPress {
				press = selection.PrimaryPress{At: time.Unix(0, 0), Point: mygo.Point{X: 10, Y: 20}}
			}
			m.actions.OnInputAutoHide = func(got selection.PrimaryPress) {
				calls++
				if got != press {
					t.Errorf("dismissal press = %+v, want %+v", got, press)
				}
			}
			m.commitTransientBubbleHide(b, press, func(*bubbleView, RunStoppedMsg) { hidden++ })
			if hidden != 1 || calls != tc.wantCalls {
				t.Fatalf("hidden=%d OnInputAutoHide calls=%d, want hidden=1 calls=%d", hidden, calls, tc.wantCalls)
			}
		})
	}
}
