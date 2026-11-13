package bubble

import (
	"context"
	"testing"

	"github.com/egoist/mygo"

	"sotto/internal/config"
)

func TestVisibleInputReusePreservesSessionAndWork(t *testing.T) {
	var gate testGate
	entry := gate.Begin()
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	b := &bubble{win: w, canShow: entry.Valid}
	m.byID[1], m.transient = b, b
	b.replaceContent(config.Default(), "input", "submitted text", "", "", nil)
	b.present()
	b.ready = true
	b.claimShow()
	ctx, ok := m.StartRun(w, RunReq{SessionID: b.sessionID, ReqID: "run", Revision: 1, Text: "submitted text", SourceLang: "en", TargetLang: "ja"})
	if !ok {
		t.Fatal("input translation did not start")
	}
	session, show, revision := b.sessionID, b.showID, b.revision
	if !b.refocusInput(true) || !b.canShow() || ctx.Err() != nil {
		t.Fatal("visible current input should preserve its work")
	}
	if b.sessionID != session || b.showID != show+1 || b.revision != revision || b.targetLang != "ja" || !b.presentationShown {
		t.Fatal("refocus changed content or reset first-show state")
	}
	if b.claimClose(session, show, revision+1) || b.shouldHideAfterBlur(show, true, false) {
		t.Fatal("old presentation callback affected refocused input")
	}
	if b.claimShow() || ctx.Err() != nil {
		t.Fatal("refocused input repeated first presentation or canceled translation")
	}
	// A newer acquisition can leave the old input window visible. It must not
	// consume panel entry as a no-op or the pending selection would later win.
	next := gate.Begin()
	if b.canShow() || !next.Valid() || ctx.Err() != nil {
		t.Fatal("new acquisition must revoke entry ownership, not independent translation")
	}
}

func TestInputFocusRequiresVisibleReusablePresentation(t *testing.T) {
	for _, blocked := range []string{"hidden", "pinned", "closed", "not-shown", "selection", "ocr"} {
		t.Run(blocked, func(t *testing.T) {
			b := &bubble{trigger: "input", presentationShown: true}
			visible := true
			switch blocked {
			case "hidden":
				visible = false
			case "pinned":
				b.pinned = true
			case "closed":
				b.closeClaimed = true
			case "not-shown":
				b.presentationShown = false
			default:
				b.trigger = blocked
			}
			if b.refocusInput(visible) {
				t.Fatal("non-current input presentation was reused")
			}
		})
	}
}

func TestPinReclaimsTransientSlotAndRevokesDisplacedWork(t *testing.T) {
	m := NewManager(nil, Actions{})
	cfg := config.Default()
	input := &bubble{id: 1, win: &mygo.Window{}, sessionID: "input", showID: 2, presentationShown: true}
	m.byID[input.id], m.transient = input, input
	if b, _ := m.changePin(cfg, input.win, input.id, "old input", 2, true); b != nil {
		t.Fatal("a prior session pinned the reused window")
	}
	if b, _ := m.changePin(cfg, input.win, input.id, "input", 1, true); b != nil {
		t.Fatal("a prior presentation pinned the refocused window")
	}
	if b, _ := m.changePin(cfg, &mygo.Window{}, input.id, "input", 2, true); b != nil {
		t.Fatal("a different window pinned the input")
	}
	inputRun, ok := m.StartRun(input.win, RunReq{SessionID: "input", ReqID: "draft", Revision: 1})
	if !ok {
		t.Fatal("could not start input translation")
	}
	if b, displaced := m.changePin(cfg, input.win, input.id, "input", 2, true); b != input || displaced != nil || m.transient != nil || !input.pinned {
		t.Fatal("pinning did not free the slot and preserve the input window")
	}

	ocrCtx, Cancel := context.WithCancel(context.Background())
	defer Cancel()
	ocr := &bubble{id: 2, win: &mygo.Window{}, sessionID: "ocr", showID: 1, phase: "recognizing", revision: -1, cancelCapture: Cancel}
	m.byID[ocr.id], m.transient = ocr, ocr
	if b, displaced := m.changePin(cfg, input.win, input.id, "input", 2, false); b != input || displaced != ocr || m.transient != input || input.pinned {
		t.Fatal("unpinning did not reclaim the slot and identify the displaced window for destruction")
	}
	if ocrCtx.Err() != context.Canceled || m.CompleteCapture(cfg, Shown{Win: ocr.win, ReqID: "ocr"}, "late OCR", "") {
		t.Fatal("displaced OCR work survived slot replacement")
	}
	if inputRun.Err() != nil || input.sessionID != "input" {
		t.Fatal("pinning or unpinning changed the input session or stopped its translation")
	}
}

func TestRunOrderingAndIsolation(t *testing.T) {
	m := NewManager(nil, Actions{})
	w1, w2 := &mygo.Window{}, &mygo.Window{}
	m.byID[1] = &bubble{win: w1, sessionID: "a", revision: -1}
	m.byID[2] = &bubble{win: w2, sessionID: "pinned", revision: -1, pinned: true}
	first, ok := m.StartRun(w1, RunReq{SessionID: "a", ReqID: "first", Revision: 1})
	if !ok {
		t.Fatal("first run rejected")
	}
	pinned, _ := m.StartRun(w2, RunReq{SessionID: "pinned", ReqID: "independent", Revision: 1})
	latest, ok := m.StartRun(w1, RunReq{SessionID: "a", ReqID: "latest", Revision: 3})
	if !ok || first.Err() == nil || latest.Err() != nil || pinned.Err() != nil {
		t.Fatal("replacement must Cancel only this bubble's run")
	}
	if _, ok := m.StartRun(w1, RunReq{SessionID: "a", ReqID: "late IPC", Revision: 2}); ok {
		t.Fatal("accepted late run")
	}
	if m.RecordResult(w1, TranslationUpdateMsg{ReqID: "first", Service: "google", Text: "stale"}) {
		t.Fatal("accepted stale result")
	}
	if _, ok := m.StartRun(w1, RunReq{SessionID: "old session", ReqID: "stale", Revision: 99}); ok {
		t.Fatal("accepted prior session")
	}
	m.Cancel(w1, "a", 5)
	if latest.Err() == nil {
		t.Fatal("Cancel did not stop work")
	}
	if _, ok := m.StartRun(w1, RunReq{SessionID: "a", ReqID: "arrives after Cancel", Revision: 4}); ok {
		t.Fatal("Cancel lost to late IPC")
	}
	if pinned.Err() != nil {
		t.Fatal("Cancel affected pinned bubble")
	}
}

func TestSelectionReplacesInputSessionOnSameWindow(t *testing.T) {
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	b := &bubble{win: w}
	m.byID[1], m.transient = b, b
	b.replaceContent(config.Default(), "input", "first", "", "", nil)
	oldSession := b.sessionID
	old, ok := m.StartRun(w, RunReq{SessionID: oldSession, ReqID: "first", Revision: 0, Text: "first"})
	if !ok || !m.RecordMeta(w, RunMetaMsg{ReqID: "first", Services: []string{"google"}}, "") {
		t.Fatal("could not start first selection")
	}
	retry, ok := m.StartRun(w, RunReq{SessionID: oldSession, ReqID: "retry", Revision: 1, Service: "google"})
	if !ok {
		t.Fatal("could not start pending retry")
	}
	// This is the same content transition ShowTask uses after acquiring a new
	// selection, including when the old window was hidden before results arrived.
	b.replaceContent(config.Default(), "selection", "second", "", "", nil)
	if old.Err() != context.Canceled || retry.Err() != context.Canceled {
		t.Fatal("replacement did not Cancel all old requests before starting the new one")
	}
	if b.sessionID == oldSession || b.generation != 2 || b.text != "second" || b.trigger != "selection" {
		t.Fatal("replacement failed to claim new content")
	}
	if _, ok := m.StartRun(w, RunReq{SessionID: oldSession, ReqID: "late IPC", Revision: 99}); ok {
		t.Fatal("old page restarted work after replacement")
	}
	latest, ok := m.StartRun(w, RunReq{SessionID: b.sessionID, ReqID: "second", Revision: 0, Text: "second"})
	if !ok || latest.Err() != nil {
		t.Fatal("new selection could not start immediately")
	}
	m.RecordMeta(w, RunMetaMsg{ReqID: "second", Services: []string{"google"}}, "")
	for _, reqID := range []string{"first", "retry"} {
		if m.RecordMeta(w, RunMetaMsg{ReqID: reqID, Services: []string{"google"}}, "") ||
			m.RecordResult(w, TranslationUpdateMsg{ReqID: reqID, Service: "google", Text: "stale"}) {
			t.Fatal("late old request reclaimed the new selection")
		}
		m.FinishRun(w, reqID)
	}
	if latest.Err() != nil || !m.RecordResult(w, TranslationUpdateMsg{ReqID: "second", Service: "google", Text: "new"}) {
		t.Fatal("old completion affected the new request")
	}
	m.FinishRun(w, "second")
}

func TestRetryPreservesOtherResultsAndSnapshot(t *testing.T) {
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
	m.StartRun(w, RunReq{SessionID: "s", ReqID: "first", Revision: 0})
	m.RecordMeta(w, RunMetaMsg{ReqID: "first", Services: []string{"google", "baidu"}}, "")
	m.RecordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "google", Text: "hello"})
	m.RecordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "baidu", Err: "offline"})
	m.StartRun(w, RunReq{SessionID: "s", ReqID: "retry", Revision: 1, Service: "baidu"})
	state, _ := m.State(config.Default(), 1)
	if len(state.Results) != 1 || state.Results[0].Text != "hello" {
		t.Fatalf("retry cleared successful result: %+v", state.Results)
	}
}

func TestRetryKeepsPendingPeerAlive(t *testing.T) {
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
	original, _ := m.StartRun(w, RunReq{SessionID: "s", ReqID: "original", Revision: 0})
	m.RecordMeta(w, RunMetaMsg{ReqID: "original", Services: []string{"google", "baidu"}}, "")
	m.StartRun(w, RunReq{SessionID: "s", ReqID: "retry", Revision: 1, Service: "baidu"})
	m.RecordMeta(w, RunMetaMsg{ReqID: "retry", Services: []string{"google", "baidu"}}, "baidu")
	if original.Err() != nil {
		t.Fatal("retry canceled pending peer")
	}
	if !m.RecordResult(w, TranslationUpdateMsg{ReqID: "original", Service: "google", Text: "peer result"}) {
		t.Fatal("dropped peer's late result")
	}
	if m.RecordResult(w, TranslationUpdateMsg{ReqID: "original", Service: "baidu", Text: "old attempt"}) {
		t.Fatal("accepted superseded service attempt")
	}
	m.Cancel(w, "s", 2)
	if original.Err() == nil {
		t.Fatal("explicit Cancel left original request running")
	}
}

func TestIndependentRetriesSurviveInterleaving(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata delayed", true: "IPC reversed"}[reverse], func(t *testing.T) {
			m := NewManager(nil, Actions{})
			w := &mygo.Window{}
			m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
			m.StartRun(w, RunReq{SessionID: "s", ReqID: "full", Revision: 0})
			m.RecordMeta(w, RunMetaMsg{ReqID: "full", Services: []string{"google", "baidu"}}, "")
			requests := []RunReq{
				{SessionID: "s", ReqID: "a", Revision: 1, Service: "google"},
				{SessionID: "s", ReqID: "b", Revision: 2, Service: "baidu"},
			}
			if reverse {
				requests[0], requests[1] = requests[1], requests[0]
			}
			contexts := []context.Context{}
			for _, req := range requests {
				ctx, ok := m.StartRun(w, req)
				if !ok {
					t.Fatalf("independent retry rejected: %s", req.Service)
				}
				contexts = append(contexts, ctx)
				if m.RecordResult(w, TranslationUpdateMsg{ReqID: "full", Service: req.Service}) {
					t.Fatal("old attempt reclaimed card before retry metadata")
				}
			}
			for _, req := range requests {
				if !m.RecordMeta(w, RunMetaMsg{ReqID: req.ReqID, Services: []string{"google", "baidu"}}, req.Service) ||
					!m.RecordResult(w, TranslationUpdateMsg{ReqID: req.ReqID, Service: req.Service, Text: req.Service}) {
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
					t.Fatal("Cancel left retry running")
				}
			}
		})
	}
}

func TestRetryCannotCrossReplacementOrCancellation(t *testing.T) {
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "s", revision: -1}
	m.StartRun(w, RunReq{SessionID: "s", ReqID: "full", Revision: 0})
	m.StartRun(w, RunReq{SessionID: "s", ReqID: "new retry", Revision: 2, Service: "google"})
	if _, ok := m.StartRun(w, RunReq{SessionID: "s", ReqID: "old retry", Revision: 1, Service: "google"}); ok {
		t.Fatal("older retry of same service accepted")
	}
	// Full metadata may finish after a retry, but must not reclaim its card.
	m.RecordMeta(w, RunMetaMsg{ReqID: "full", Services: []string{"google", "baidu"}}, "")
	if m.RecordResult(w, TranslationUpdateMsg{ReqID: "full", Service: "google"}) ||
		!m.RecordResult(w, TranslationUpdateMsg{ReqID: "full", Service: "baidu"}) {
		t.Fatal("late full metadata overwrote retry ownership or lost peer")
	}
	m.StartRun(w, RunReq{SessionID: "s", ReqID: "replacement", Revision: 4})
	if _, ok := m.StartRun(w, RunReq{SessionID: "s", ReqID: "late peer", Revision: 3, Service: "baidu"}); ok {
		t.Fatal("retry crossed full replacement")
	}
	if m.RecordMeta(w, RunMetaMsg{ReqID: "new retry"}, "google") ||
		m.RecordResult(w, TranslationUpdateMsg{ReqID: "new retry", Service: "google"}) {
		t.Fatal("old retry published after replacement")
	}
	m.Cancel(w, "s", 6)
	if _, ok := m.StartRun(w, RunReq{SessionID: "s", ReqID: "late canceled", Revision: 5, Service: "baidu"}); ok {
		t.Fatal("retry crossed cancellation")
	}
}

func TestCloseRequiresCurrentPresentationAndRevision(t *testing.T) {
	for _, submit := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsubmitted presentation", true: "submitted presentation"}[submit], func(t *testing.T) {
			m := NewManager(nil, Actions{})
			w := &mygo.Window{}
			b := &bubble{win: w, sessionID: "draft", revision: -1}
			m.byID[1] = b
			b.present()
			oldShow := b.showID
			ctx, _ := m.StartRun(w, RunReq{SessionID: "draft", ReqID: "original", Revision: 0})
			b.present()
			if submit {
				ctx, _ = m.StartRun(w, RunReq{SessionID: "draft", ReqID: "new", Revision: 2})
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
				t.Fatal("accepted close did not Cancel work")
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
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	m.byID[1] = &bubble{win: w, sessionID: "stream", revision: -1}
	ctx, _ := m.StartRun(w, RunReq{SessionID: "stream", ReqID: "first", Revision: 1})
	m.RecordMeta(w, RunMetaMsg{ReqID: "first", Services: []string{"openai"}}, "")
	for _, text := range []string{"你", "你好"} {
		if !m.RecordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "openai", Text: text, Streaming: true}) {
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
	if m.RecordResult(w, TranslationUpdateMsg{ReqID: "first", Service: "openai", Text: "stale", Streaming: true}) {
		t.Fatal("canceled stream reclaimed its card")
	}
}

func TestNewInputReplacesSessionAndRejectsOldWork(t *testing.T) {
	m := NewManager(nil, Actions{})
	w := &mygo.Window{}
	b := &bubble{win: w}
	m.byID[1] = b
	cfg := config.Default()
	b.replaceContent(cfg, "input", "previous text", "", "", nil)
	b.present()
	oldSession := b.sessionID
	ctx, _ := m.StartRun(w, RunReq{SessionID: oldSession, ReqID: "old", Revision: 1, Text: "previous text"})
	m.RecordResult(w, TranslationUpdateMsg{ReqID: "old", Service: "google", Text: "previous result"})
	b.replaceContent(cfg, "input", "", "", "", nil)
	b.present()
	state := m.state(b, cfg)
	if ctx.Err() != context.Canceled || state.SessionID == oldSession || state.Text != "" || len(state.Results) != 0 || state.Meta != nil || len(state.ServiceRequests) != 0 {
		t.Fatalf("new input retained old content/work: %+v", state)
	}
	if state.Generation != 2 || state.ShowID != 2 || state.Revision != -1 || b.presentationShown {
		t.Fatal("new input did not acquire a fresh unsubmitted presentation")
	}
	if _, ok := m.StartRun(w, RunReq{SessionID: oldSession, ReqID: "late", Revision: 99}); ok || m.RecordResult(w, TranslationUpdateMsg{ReqID: "old", Service: "google", Text: "late"}) {
		t.Fatal("old input work entered the fresh session")
	}
}

func TestOCRCompletionBeforeFirstShowPreservesNativeReadiness(t *testing.T) {
	m := NewManager(nil, Actions{})
	b := &bubble{id: 1, win: &mygo.Window{}}
	m.byID[b.id] = b
	b.replaceContent(config.Default(), "ocr", "", "recognizing", "", nil)
	b.present()
	session, show, generation := b.sessionID, b.showID, b.generation
	if !m.CompleteCapture(config.Default(), Shown{Win: b.win, ReqID: session}, "recognized text", "") {
		t.Fatal("current capture completion rejected")
	}
	if b.text != "recognized text" || b.phase != "" || b.sessionID != session || b.showID != show || b.generation != generation {
		t.Fatal("capture completion replaced presentation ownership or lost recognized text")
	}
	if b.claimShow() {
		t.Fatal("capture completion bypassed native readiness")
	}
	b.ready = true
	if !b.claimShow() || b.claimShow() {
		t.Fatal("native readiness must release the completed capture exactly once")
	}
}

func TestFirstPresentationRequiresReadinessAndCannotRepeat(t *testing.T) {
	b := &bubble{sessionID: "s"}
	b.present()
	if b.claimShow() {
		t.Fatal("presentation bypassed native readiness")
	}
	b.ready = true
	if !b.claimShow() || b.claimShow() {
		t.Fatal("ready presentation must show exactly once")
	}
	b.stopPresentation()
	if b.claimShow() {
		t.Fatal("stopped presentation reopened")
	}
	b.replaceContent(config.Default(), "input", "", "", "", nil)
	b.present()
	if !b.claimShow() || b.claimShow() {
		t.Fatal("replacement must reuse native readiness and show exactly once")
	}
	if (&bubble{ready: true}).claimShow() {
		t.Fatal("window without an accepted presentation showed")
	}
}

func TestExpiredEntryCannotShowAfterNativeReadiness(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired entry", true: "closed presentation"}[closed], func(t *testing.T) {
			m := NewManager(nil, Actions{})
			var gate testGate
			entry := gate.Begin()
			b := &bubble{win: &mygo.Window{}, canShow: entry.Valid}
			b.present()
			if closed {
				b.stopPresentation()
			} else {
				entry.Cancel()
			}
			b.ready = true
			// showWin must reject before touching a native window. A zero Window
			// makes accidental native presentation fail in this state-only test.
			m.showWin(b)
			if b.presentationShown {
				t.Fatal("native readiness revived an expired or closed presentation")
			}
		})
	}
}

// Each new acquisition cancels only the previous entry, independently of
// translation jobs owned by a pinned or already presented bubble.
type testGate struct{ cancel context.CancelFunc }
type testTask struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (g *testGate) Begin() *testTask {
	if g.cancel != nil {
		g.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	return &testTask{ctx: ctx, cancel: cancel}
}
func (t *testTask) Cancel()     { t.cancel() }
func (t *testTask) Valid() bool { return t.ctx.Err() == nil }
