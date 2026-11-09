package main

import (
	"context"
	"testing"

	"github.com/egoist/mygo"

	"sotto/internal/config"
)

func TestEntryOwnership(t *testing.T) {
	var g entryGate
	old := g.begin()
	next := g.begin()
	if old.valid() || old.ctx.Err() == nil || !next.valid() {
		t.Fatal("new entry must revoke old acquisition")
	}
	old.cancel()
	if !next.valid() {
		t.Fatal("old cancellation revoked the new entry")
	}
	next.cancel()
	if next.valid() {
		t.Fatal("canceled entry still owns the UI")
	}
}

func TestRunOrderingAndIsolation(t *testing.T) {
	m := newBubbleManager(nil)
	w1, w2 := &mygo.Window{}, &mygo.Window{}
	m.byID[1] = &bubble{win: w1, sessionID: "a", revision: -1}
	m.byID[2] = &bubble{win: w2, sessionID: "pinned", revision: -1, pinned: true}
	first, ok := m.startRun(w1, RunReq{SessionID: "a", ReqID: "first", Revision: 1})
	if !ok {
		t.Fatal("first run rejected")
	}
	pinned, _ := m.startRun(w2, RunReq{SessionID: "pinned", ReqID: "independent", Revision: 1})
	latest, ok := m.startRun(w1, RunReq{SessionID: "a", ReqID: "latest", Revision: 3})
	if !ok || first.Err() == nil || latest.Err() != nil || pinned.Err() != nil {
		t.Fatal("replacement must cancel only this bubble's run")
	}
	if _, ok := m.startRun(w1, RunReq{SessionID: "a", ReqID: "late IPC", Revision: 2}); ok {
		t.Fatal("accepted late run")
	}
	if m.recordResult(w1, TranslationUpdateMsg{ReqID: "first", Service: "google", Text: "stale"}) {
		t.Fatal("accepted stale result")
	}
	if _, ok := m.startRun(w1, RunReq{SessionID: "old session", ReqID: "stale", Revision: 99}); ok {
		t.Fatal("accepted prior session")
	}
	m.Cancel(w1, "a", 5)
	if latest.Err() == nil {
		t.Fatal("cancel did not stop work")
	}
	if _, ok := m.startRun(w1, RunReq{SessionID: "a", ReqID: "arrives after cancel", Revision: 4}); ok {
		t.Fatal("cancel lost to late IPC")
	}
	if pinned.Err() != nil {
		t.Fatal("cancel affected pinned bubble")
	}
}

func TestNewSelectionCancelsPendingTranslationBeforeReplacementStarts(t *testing.T) {
	m := newBubbleManager(nil)
	w := &mygo.Window{}
	b := &bubble{win: w}
	m.byID[1] = b
	b.replaceContent(config.Default(), "selection", "first", "", "", nil)
	oldSession := b.sessionID
	old, ok := m.startRun(w, RunReq{SessionID: oldSession, ReqID: "first", Revision: 0, Text: "first"})
	if !ok || !m.recordMeta(w, RunMetaMsg{ReqID: "first", Services: []string{"google"}}, "") {
		t.Fatal("could not start first selection")
	}
	retry, ok := m.startRun(w, RunReq{SessionID: oldSession, ReqID: "retry", Revision: 1, Service: "google"})
	if !ok {
		t.Fatal("could not start pending retry")
	}
	// This is the same content transition ShowTask uses after acquiring a new
	// selection, including when the old window was hidden before results arrived.
	b.replaceContent(config.Default(), "selection", "second", "", "", nil)
	if old.Err() != context.Canceled || retry.Err() != context.Canceled {
		t.Fatal("replacement did not cancel all old requests before starting the new one")
	}
	if b.sessionID == oldSession || b.generation != 2 || b.text != "second" {
		t.Fatal("replacement failed to claim new content")
	}
	if _, ok := m.startRun(w, RunReq{SessionID: oldSession, ReqID: "late IPC", Revision: 99}); ok {
		t.Fatal("old page restarted work after replacement")
	}
	latest, ok := m.startRun(w, RunReq{SessionID: b.sessionID, ReqID: "second", Revision: 0, Text: "second"})
	if !ok || latest.Err() != nil {
		t.Fatal("new selection could not start immediately")
	}
	m.recordMeta(w, RunMetaMsg{ReqID: "second", Services: []string{"google"}}, "")
	for _, reqID := range []string{"first", "retry"} {
		if m.recordMeta(w, RunMetaMsg{ReqID: reqID, Services: []string{"google"}}, "") ||
			m.recordResult(w, TranslationUpdateMsg{ReqID: reqID, Service: "google", Text: "stale"}) {
			t.Fatal("late old request reclaimed the new selection")
		}
		m.finishRun(w, reqID)
	}
	if latest.Err() != nil || !m.recordResult(w, TranslationUpdateMsg{ReqID: "second", Service: "google", Text: "new"}) {
		t.Fatal("old completion affected the new request")
	}
	m.finishRun(w, "second")
}

func TestRetryPreservesOtherResultsAndSnapshot(t *testing.T) {
	m := newBubbleManager(nil)
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
	m.startRun(w, RunReq{SessionID: "s", ReqID: "first", Revision: 0})
	m.recordMeta(w, RunMetaMsg{ReqID: "first", Services: []string{"google", "baidu"}}, "")
	m.recordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "google", Text: "hello"})
	m.recordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "baidu", Err: "offline"})
	m.startRun(w, RunReq{SessionID: "s", ReqID: "retry", Revision: 1, Service: "baidu"})
	state, _ := m.State(config.Default(), 1)
	if len(state.Results) != 1 || state.Results[0].Text != "hello" {
		t.Fatalf("retry cleared successful result: %+v", state.Results)
	}
}

func TestExplicitTargetWins(t *testing.T) {
	cases := []struct{ script, requested, configured, want string }{
		{"latin", "en", "zh-HANS", "en"},
		{"zh", "zh-HANS", "en", "zh-HANS"},
		{"latin", "", "en", "en"},
		{"zh", "", "zh-HANS", "en"},
		{"zh", "auto", "zh-HANS", "en"},
		{"latin", "auto", "en", "zh-HANS"},
	}
	for _, c := range cases {
		if got := resolveTarget(c.script, c.requested, c.configured); got != c.want {
			t.Errorf("%+v: got %s", c, got)
		}
	}
}

func TestRetryKeepsPendingPeerAlive(t *testing.T) {
	m := newBubbleManager(nil)
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
	original, _ := m.startRun(w, RunReq{SessionID: "s", ReqID: "original", Revision: 0})
	m.recordMeta(w, RunMetaMsg{ReqID: "original", Services: []string{"google", "baidu"}}, "")
	m.startRun(w, RunReq{SessionID: "s", ReqID: "retry", Revision: 1, Service: "baidu"})
	m.recordMeta(w, RunMetaMsg{ReqID: "retry", Services: []string{"google", "baidu"}}, "baidu")
	if original.Err() != nil {
		t.Fatal("retry canceled pending peer")
	}
	if !m.recordResult(w, TranslationUpdateMsg{ReqID: "original", Service: "google", Text: "peer result"}) {
		t.Fatal("dropped peer's late result")
	}
	if m.recordResult(w, TranslationUpdateMsg{ReqID: "original", Service: "baidu", Text: "old attempt"}) {
		t.Fatal("accepted superseded service attempt")
	}
	m.Cancel(w, "s", 2)
	if original.Err() == nil {
		t.Fatal("explicit cancel left original request running")
	}
}

func TestIndependentRetriesSurviveInterleaving(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata delayed", true: "IPC reversed"}[reverse], func(t *testing.T) {
			m := newBubbleManager(nil)
			w := &mygo.Window{}
			m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
			m.startRun(w, RunReq{SessionID: "s", ReqID: "full", Revision: 0})
			m.recordMeta(w, RunMetaMsg{ReqID: "full", Services: []string{"google", "baidu"}}, "")
			requests := []RunReq{
				{SessionID: "s", ReqID: "a", Revision: 1, Service: "google"},
				{SessionID: "s", ReqID: "b", Revision: 2, Service: "baidu"},
			}
			if reverse {
				requests[0], requests[1] = requests[1], requests[0]
			}
			contexts := []context.Context{}
			for _, req := range requests {
				ctx, ok := m.startRun(w, req)
				if !ok {
					t.Fatalf("independent retry rejected: %s", req.Service)
				}
				contexts = append(contexts, ctx)
				if m.recordResult(w, TranslationUpdateMsg{ReqID: "full", Service: req.Service}) {
					t.Fatal("old attempt reclaimed card before retry metadata")
				}
			}
			for _, req := range requests {
				if !m.recordMeta(w, RunMetaMsg{ReqID: req.ReqID, Services: []string{"google", "baidu"}}, req.Service) ||
					!m.recordResult(w, TranslationUpdateMsg{ReqID: req.ReqID, Service: req.Service, Text: req.Service}) {
					t.Fatalf("lost retry metadata or result: %s", req.Service)
				}
			}
			state, _ := m.State(config.Default(), 1)
			if len(state.Results) != 2 || state.Revision != 2 || state.ReqID != "b" {
				t.Fatalf("snapshot regressed: %+v", state)
			}
			m.Cancel(w, "s", 3)
			for _, ctx := range contexts {
				if ctx.Err() == nil {
					t.Fatal("cancel left retry running")
				}
			}
		})
	}
}

func TestRetryCannotCrossReplacementOrCancellation(t *testing.T) {
	m := newBubbleManager(nil)
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
	m.startRun(w, RunReq{SessionID: "s", ReqID: "full", Revision: 0})
	m.startRun(w, RunReq{SessionID: "s", ReqID: "new retry", Revision: 2, Service: "google"})
	if _, ok := m.startRun(w, RunReq{SessionID: "s", ReqID: "old retry", Revision: 1, Service: "google"}); ok {
		t.Fatal("older retry of same service accepted")
	}
	// Full metadata may finish after a retry, but must not reclaim its card.
	m.recordMeta(w, RunMetaMsg{ReqID: "full", Services: []string{"google", "baidu"}}, "")
	if m.recordResult(w, TranslationUpdateMsg{ReqID: "full", Service: "google"}) ||
		!m.recordResult(w, TranslationUpdateMsg{ReqID: "full", Service: "baidu"}) {
		t.Fatal("late full metadata overwrote retry ownership or lost peer")
	}
	m.startRun(w, RunReq{SessionID: "s", ReqID: "replacement", Revision: 4})
	if _, ok := m.startRun(w, RunReq{SessionID: "s", ReqID: "late peer", Revision: 3, Service: "baidu"}); ok {
		t.Fatal("retry crossed full replacement")
	}
	if m.recordMeta(w, RunMetaMsg{ReqID: "new retry"}, "google") ||
		m.recordResult(w, TranslationUpdateMsg{ReqID: "new retry", Service: "google"}) {
		t.Fatal("old retry published after replacement")
	}
	m.Cancel(w, "s", 6)
	if _, ok := m.startRun(w, RunReq{SessionID: "s", ReqID: "late canceled", Revision: 5, Service: "baidu"}); ok {
		t.Fatal("retry crossed cancellation")
	}
}

func TestCloseRequiresCurrentPresentationAndRevision(t *testing.T) {
	for _, submit := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsubmitted presentation", true: "submitted presentation"}[submit], func(t *testing.T) {
			m := newBubbleManager(nil)
			w := &mygo.Window{}
			b := &bubble{win: w, sessionID: "draft", revision: -1}
			m.byID[1] = b
			b.present()
			oldShow := b.showID
			ctx, _ := m.startRun(w, RunReq{SessionID: "draft", ReqID: "original", Revision: 0})
			b.present()
			if submit {
				ctx, _ = m.startRun(w, RunReq{SessionID: "draft", ReqID: "new", Revision: 2})
			}
			before := b.revision
			if b.claimClose("draft", oldShow, 1) || b.revision != before || ctx.Err() != nil {
				t.Fatal("late close affected current presentation or request")
			}
			if b.claimClose("old session", b.showID, 99) || b.claimClose("draft", b.showID, before) {
				t.Fatal("accepted stale session/revision")
			}
			if !b.claimClose("draft", b.showID, before+1) {
				t.Fatal("current close rejected")
			}
			b.stopWork()
			if ctx.Err() == nil {
				t.Fatal("accepted close did not cancel work")
			}
			if b.claimClose("draft", b.showID, before+2) {
				t.Fatal("duplicate close accepted")
			}
			b.present()
			if !b.claimClose("draft", b.showID, before+2) {
				t.Fatal("new presentation cannot close")
			}
		})
	}
}

func TestDelayedBlurCannotHideANewerSelection(t *testing.T) {
	b := &bubble{offBlur: func() {}}
	b.present()
	previous := b.showID
	if !b.shouldHideAfterBlur(previous, true, false) {
		t.Fatal("real outside focus change must hide")
	}
	if b.shouldHideAfterBlur(previous, true, true) {
		t.Fatal("stale blur hid a refocused window")
	}
	b.present()
	if b.shouldHideAfterBlur(previous, true, false) {
		t.Fatal("previous selection's blur hid the new selection")
	}
	if b.shouldHideAfterBlur(b.showID, false, false) {
		t.Fatal("hidden window was hidden again")
	}
	b.pinned = true
	if b.shouldHideAfterBlur(b.showID, true, false) {
		t.Fatal("blur hid a pinned window")
	}
	b.pinned, b.offBlur = false, nil
	if b.shouldHideAfterBlur(b.showID, true, false) {
		t.Fatal("blur ignored disabled auto-hide")
	}
}

func TestStreamingSnapshotsAndCancellation(t *testing.T) {
	m := newBubbleManager(nil)
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "stream", revision: -1}
	ctx, _ := m.startRun(w, RunReq{SessionID: "stream", ReqID: "first", Revision: 1})
	m.recordMeta(w, RunMetaMsg{ReqID: "first", Services: []string{"openai"}}, "")
	for _, text := range []string{"你", "你好"} {
		if !m.recordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "openai", Text: text, Streaming: true}) {
			t.Fatal("stream update rejected")
		}
	}
	state, _ := m.State(config.Default(), 1)
	if len(state.Results) != 1 || state.Results[0].Text != "你好" || !state.Results[0].Streaming {
		t.Fatalf("Ready must hold only the latest cumulative snapshot: %+v", state.Results)
	}
	m.Cancel(w, "stream", 2)
	state, _ = m.State(config.Default(), 1)
	if ctx.Err() == nil || state.Results[0].Streaming || state.Results[0].Text != "你好" || state.Results[0].Err == "" {
		t.Fatalf("canceled snapshot must retain text and settle: %+v", state.Results)
	}
	if m.recordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "openai", Text: "stale", Streaming: true}) {
		t.Fatal("canceled stream reclaimed its card")
	}
}

func TestNewInputReplacesSessionAndRejectsOldWork(t *testing.T) {
	m := newBubbleManager(nil)
	w := &mygo.Window{}
	b := &bubble{win: w}
	m.byID[1] = b
	cfg := config.Default()
	b.replaceContent(cfg, "input", "previous text", "", "", nil)
	b.present()
	oldSession := b.sessionID
	ctx, _ := m.startRun(w, RunReq{SessionID: oldSession, ReqID: "old", Revision: 1, Text: "previous text"})
	m.recordResult(w, TranslationUpdateMsg{ReqID: "old", Service: "google", Text: "previous result"})
	b.replaceContent(cfg, "input", "", "", "", nil)
	b.present()
	state := m.state(b, cfg)
	if ctx.Err() != context.Canceled || state.SessionID == oldSession || state.Text != "" || len(state.Results) != 0 || state.Meta != nil || len(state.ServiceRequests) != 0 {
		t.Fatalf("new input retained old content/work: %+v", state)
	}
	if !b.awaitingLayout || state.LayoutID != 2 || state.Generation != 2 {
		t.Fatal("new input did not require a fresh layout")
	}
	if _, ok := m.startRun(w, RunReq{SessionID: oldSession, ReqID: "late", Revision: 99}); ok || m.recordResult(w, TranslationUpdateMsg{ReqID: "old", Service: "google", Text: "late"}) {
		t.Fatal("old input work entered the fresh session")
	}
}

func TestLayoutAcknowledgementOwnership(t *testing.T) {
	for _, stale := range []string{"session", "show", "layout", "closed"} {
		t.Run(stale, func(t *testing.T) {
			b := &bubble{sessionID: "current", showID: 2, layoutID: 3, awaitingLayout: true}
			session, show, layout := "current", int64(2), int64(3)
			switch stale {
			case "session":
				session = "old"
			case "show":
				show--
			case "layout":
				layout-- // OCR completed before recognition layout arrived.
			case "closed":
				b.closeClaimed = true
			}
			if b.acceptLayout(session, show, layout) || !b.awaitingLayout {
				t.Fatal("stale acknowledgement released the presentation")
			}
			b.closeClaimed = false
			if !b.acceptLayout("current", 2, 3) || b.awaitingLayout || b.acceptLayout("current", 2, 3) {
				t.Fatal("current layout must release presentation exactly once")
			}
		})
	}
}

func TestOCRCompletionBeforeFirstShowRequiresReplacementLayout(t *testing.T) {
	b := &bubble{sessionID: "ocr", showID: 1, layoutID: 1, awaitingLayout: true}
	if !b.acceptLayout("ocr", 1, 1) {
		t.Fatal("recognition layout rejected")
	}
	// Native readiness has not arrived yet; accepting a layout is not a show.
	b.advanceCaptureLayout()
	if !b.awaitingLayout || b.acceptLayout("ocr", 1, 1) || !b.acceptLayout("ocr", 1, 2) {
		t.Fatal("OCR completion reused the hidden recognition layout")
	}
	b.presentationShown = true
	b.advanceCaptureLayout()
	if b.awaitingLayout {
		t.Fatal("visible OCR completion must continue using FitContent")
	}
}
