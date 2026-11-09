package main

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/egoist/mygo"

	"sotto/internal/config"
)

func newDismissTestBubble(t *testing.T, trigger, phase string) (*bubbleManager, *bubble) {
	t.Helper()
	m := newBubbleManager(nil)
	b := &bubble{id: 1, win: &mygo.Window{}, ready: true}
	m.byID[b.id] = b
	b.replaceContent(config.Default(), trigger, "original source", phase, "", nil)
	b.present()
	b.canShow = func() bool { return true }
	t.Cleanup(b.stopWork)
	return m, b
}

func equalDismissTestSnapshots(before, after BubbleState) bool {
	// resultList walks a map; card ordering is not part of the state contract.
	less := func(results []TranslationUpdateMsg) {
		sort.Slice(results, func(i, j int) bool { return results[i].Service < results[j].Service })
	}
	less(before.Results)
	less(after.Results)
	return reflect.DeepEqual(before, after)
}

func dismissTestBubble(m *bubbleManager, b *bubble) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return b.dismiss()
}

func startDismissTestJobs(t *testing.T, m *bubbleManager, b *bubble) (context.Context, context.Context) {
	t.Helper()
	full, ok := m.startRun(b.win, RunReq{
		SessionID: b.sessionID, ReqID: "full", Revision: 0,
		Text: b.text, SourceLang: "en", TargetLang: "zh-HANS",
	})
	if !ok || !m.recordMeta(b.win, RunMetaMsg{
		ReqID: "full", Services: []string{"openai", "google", "baidu"},
		SourceLang: "en", TargetLang: "zh-HANS",
	}, "") {
		t.Fatal("could not start full translation")
	}
	retry, ok := m.startRun(b.win, RunReq{
		SessionID: b.sessionID, ReqID: "retry", Revision: 1, Service: "baidu",
		Text: b.text, SourceLang: "en", TargetLang: "zh-HANS",
	})
	if !ok || !m.recordMeta(b.win, RunMetaMsg{
		ReqID: "retry", Services: []string{"baidu"}, SourceLang: "en", TargetLang: "zh-HANS",
	}, "baidu") {
		t.Fatal("could not start concurrent service retry")
	}
	for _, result := range []TranslationUpdateMsg{
		{ReqID: "full", Service: "openai", Text: "partial translation", Streaming: true},
		{ReqID: "full", Service: "google", Text: "finished translation"},
		{ReqID: "retry", Service: "baidu", Text: "partial retry", Streaming: true},
	} {
		if !m.recordResult(b.win, result) {
			t.Fatalf("could not record %s result", result.Service)
		}
	}
	return full, retry
}

func TestBubbleDismissCancelsAllJobsAndSettlesSnapshot(t *testing.T) {
	for _, trigger := range []string{"input", "selection", "ocr"} {
		t.Run(trigger, func(t *testing.T) {
			m, b := newDismissTestBubble(t, trigger, "")
			full, retry := startDismissTestJobs(t, m, b)
			// Attach an independently pending acquisition after startRun, which
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
			state, ok := m.State(config.Default(), b.id)
			if !ok || state.ReqID != "" || len(state.ServiceRequests) != 0 || len(b.runCancels) != 0 || b.cancelCapture != nil {
				t.Fatalf("dismissal retained active request ownership: %+v", state)
			}
			if !b.closeClaimed || b.canShow == nil || b.canShow() || state.Revision != before.Revision+1 || b.fullRevision != state.Revision || b.fullReqID != "" {
				t.Fatal("dismissal did not revoke presentation and full-run ownership exactly once")
			}
			if state.SessionID != before.SessionID || state.ShowID != before.ShowID || state.Generation != before.Generation || state.Text != before.Text {
				t.Fatal("dismissal replaced content instead of retaining the stopped snapshot")
			}
			results := make(map[string]TranslationUpdateMsg)
			for _, result := range state.Results {
				results[result.Service] = result
			}
			if len(results) != 3 {
				t.Fatalf("dismissal lost existing cards: %+v", results)
			}
			for service, text := range map[string]string{"openai": "partial translation", "baidu": "partial retry"} {
				result := results[service]
				if result.Text != text || result.Streaming || result.Err == "" {
					t.Errorf("dismissed stream must retain partial text and settle: %+v", result)
				}
			}
			if result := results["google"]; result.Text != "finished translation" || result.Streaming || result.Err != "" {
				t.Errorf("dismissal changed a finished card: %+v", result)
			}
		})
	}
}

func TestBubbleDismissRecognitionRejectsLateCaptureAndLayout(t *testing.T) {
	for _, trigger := range []string{"selection", "ocr"} {
		t.Run(trigger, func(t *testing.T) {
			m, b := newDismissTestBubble(t, trigger, "recognizing")
			capture, cancel := context.WithCancel(context.Background())
			b.cancelCapture = cancel
			shown := Shown{Win: b.win, ReqID: b.sessionID, Text: b.text}
			// Dismiss after layout acknowledgement but before the first show,
			// so claimShow must honor closure rather than an unready layout.
			if !b.acceptLayout(b.sessionID, b.showID, b.layoutID, 1) {
				t.Fatal("recognition layout rejected")
			}
			if !dismissTestBubble(m, b) || capture.Err() != context.Canceled {
				t.Fatal("recognition dismissal did not cancel capture")
			}
			before := m.state(b, config.Default())
			if before.Phase != "error" || before.Message == "" {
				t.Fatalf("dismissed recognition still appears pending: %+v", before)
			}
			for _, message := range []string{"", "late recognition error"} {
				if m.completeCapture(config.Default(), shown, "late recognized text", message) {
					t.Fatal("dismissed capture completed into the stopped session")
				}
			}
			if b.acceptLayout(b.sessionID, b.showID, b.layoutID, b.layoutSequence+100) || b.claimShow() {
				t.Fatal("late recognition layout revived the dismissed presentation")
			}
			if after := m.state(b, config.Default()); !equalDismissTestSnapshots(before, after) {
				t.Fatalf("late capture or layout mutated the stopped snapshot: before=%+v after=%+v", before, after)
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
	if m.completeCapture(config.Default(), shown, "late recognized text", "") {
		t.Fatal("capture ignored closure while its recognition revision still matched")
	}
	if after := m.state(b, config.Default()); !equalDismissTestSnapshots(before, after) {
		t.Fatal("rejected capture changed the closed presentation")
	}
}

func TestBubbleDismissRejectsLateWorkAtAnyRevision(t *testing.T) {
	m, b := newDismissTestBubble(t, "input", "")
	startDismissTestJobs(t, m, b)
	if !dismissTestBubble(m, b) {
		t.Fatal("dismissal rejected")
	}
	before := m.state(b, config.Default())
	for _, attempt := range []struct{ request, service string }{{"full", ""}, {"retry", "baidu"}} {
		if m.recordMeta(b.win, RunMetaMsg{ReqID: attempt.request, Services: []string{"google", "baidu"}}, attempt.service) {
			t.Errorf("accepted late metadata for %s", attempt.request)
		}
		for _, service := range []string{"openai", "google", "baidu"} {
			if m.recordResult(b.win, TranslationUpdateMsg{ReqID: attempt.request, Service: service, Text: "late", Streaming: true}) {
				t.Errorf("accepted late %s result for %s", service, attempt.request)
			}
		}
		for _, revision := range []int64{before.Revision - 1, before.Revision, before.Revision + 1, 1 << 60} {
			ctx, ok := m.startRun(b.win, RunReq{
				SessionID: b.sessionID, ReqID: "late request", Revision: revision,
				Service: attempt.service, Text: "late source", SourceLang: "ja", TargetLang: "en",
			})
			if ok || ctx != nil {
				t.Fatalf("dismissed presentation accepted service %q at revision %d", attempt.service, revision)
			}
		}
		m.finishRun(b.win, attempt.request)
	}
	if b.acceptLayout(b.sessionID, b.showID, b.layoutID, b.layoutSequence+1) || b.claimShow() {
		t.Fatal("late layout reopened the dismissed bubble")
	}
	if after := m.state(b, config.Default()); !equalDismissTestSnapshots(before, after) || len(b.runCancels) != 0 {
		t.Fatalf("late work changed the stopped snapshot: before=%+v after=%+v", before, after)
	}
}

func TestBubbleDismissIsIdempotentAndPinnedDismissIsNoOp(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(map[bool]string{false: "repeated", true: "pinned"}[pinned], func(t *testing.T) {
			m, b := newDismissTestBubble(t, "selection", "")
			full, retry := startDismissTestJobs(t, m, b)
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
			if after := m.state(b, config.Default()); !equalDismissTestSnapshots(before, after) {
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
			startDismissTestJobs(t, m, b)
			oldSession, oldShow, oldLayout := b.sessionID, b.showID, b.layoutID
			if !dismissTestBubble(m, b) {
				t.Fatal("dismissal rejected")
			}
			b.replaceContent(config.Default(), trigger, "replacement source", "", "", nil)
			b.present()
			b.canShow = func() bool { return true }
			if b.closeClaimed || b.sessionID == oldSession || b.showID <= oldShow || !b.awaitingLayout {
				t.Fatal("replacement did not acquire a fresh presentation")
			}
			for _, service := range []string{"", "google"} {
				if _, ok := m.startRun(b.win, RunReq{SessionID: oldSession, ReqID: "old late", Revision: 1 << 60, Service: service}); ok {
					t.Fatal("old session entered the replacement")
				}
			}
			if b.acceptLayout(oldSession, oldShow, oldLayout, 100) || b.claimClose(oldSession, oldShow, 1<<60) || m.completeCapture(config.Default(), Shown{Win: b.win, ReqID: oldSession}, "old capture", "") {
				t.Fatal("old presentation callback affected replacement")
			}
			full, retry := startDismissTestJobs(t, m, b)
			if full.Err() != nil || retry.Err() != nil || b.text != "replacement source" {
				t.Fatal("replacement failed to start fresh full and retry jobs")
			}
			if b.claimShow() || !b.acceptLayout(b.sessionID, b.showID, b.layoutID, 1) || !b.claimShow() {
				t.Fatal("replacement did not require and accept its own first-show layout")
			}
		})
	}
}

func TestBubbleLayoutStateTransitionsDoNotDismissJobs(t *testing.T) {
	m, b := newDismissTestBubble(t, "ocr", "recognizing")
	capture, cancel := context.WithCancel(context.Background())
	b.cancelCapture = cancel
	if !b.acceptLayout(b.sessionID, b.showID, b.layoutID, 1) {
		t.Fatal("recognition layout rejected")
	}
	b.advanceCaptureLayout()
	if capture.Err() != nil || b.closeClaimed || !b.canShow() || b.revision != -1 || !b.awaitingLayout {
		t.Fatal("recognition layout transition dismissed pending capture")
	}
	full, retry := startDismissTestJobs(t, m, b)
	if !b.acceptLayout(b.sessionID, b.showID, b.layoutID, 2) || !b.claimShow() || !b.acceptLayout(b.sessionID, b.showID, b.layoutID, 3) {
		t.Fatal("current translation layout rejected")
	}
	if full.Err() != nil || retry.Err() != nil || b.closeClaimed || !b.canShow() {
		t.Fatal("translation layout transition dismissed active jobs")
	}
	// These are state-only checks. Native Hide during layout/reuse must be
	// covered by desktop e2e; a zero Window cannot establish callback behavior.
}
