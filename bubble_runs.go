package main

import (
	"context"

	"github.com/egoist/mygo"

	"sotto/internal/config"
)

// replaceContent commits a new input/selection/OCR session under mu. Cancel first,
// then revoke every old result identity before the replacement can start.
// Input entries also replace drafts instead of reviving a hidden session.
func (b *bubble) replaceContent(cfg config.Settings, trigger, text, phase, message string, cancel context.CancelFunc) {
	b.stopWork()
	b.generation++
	b.layoutID++
	b.layoutSequence = 0
	b.awaitingLayout = true
	b.sessionID = newRequestID()
	b.reqID = b.sessionID
	b.revision = -1
	b.fullRevision, b.fullReqID = -1, ""
	b.serviceRevisions = nil
	b.trigger, b.text = trigger, text
	b.sourceLang, b.targetLang = "auto", cfg.TargetLanguage
	b.phase, b.message = phase, message
	b.meta, b.results = nil, nil
	b.serviceRequests = nil
	b.cancelCapture = cancel
}

func (b *bubble) resultList() []TranslationUpdateMsg {
	out := make([]TranslationUpdateMsg, 0, len(b.results))
	for _, r := range b.results {
		out = append(out, r)
	}
	return out
}

// startRun rejects late IPC calls from an older page session or revision.
func (m *bubbleManager) startRun(win *mygo.Window, req RunReq) (context.Context, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.byID {
		if b.win != win {
			continue
		}
		if b.closeClaimed || req.SessionID != b.sessionID {
			return nil, false
		}
		if req.Service == "" {
			if req.Revision <= b.revision {
				return nil, false
			}
			b.stopWork()
			b.fullRevision, b.fullReqID = req.Revision, req.ReqID
			b.serviceRequests = make(map[string]string)
			b.serviceRevisions = make(map[string]int64)
		} else {
			// Independent retry IPCs may arrive out of order. Only a newer full
			// run, cancellation, or retry of this same service supersedes them.
			previous, exists := b.serviceRevisions[req.Service]
			if req.Revision <= b.fullRevision || (exists && req.Revision <= previous) {
				return nil, false
			}
			if b.serviceRevisions == nil {
				b.serviceRevisions = make(map[string]int64)
			}
			if b.serviceRequests == nil {
				b.serviceRequests = make(map[string]string)
			}
			// Claim the card before credential lookup, so late metadata/results
			// cannot reclaim it for an older attempt.
			b.serviceRevisions[req.Service] = req.Revision
			b.serviceRequests[req.Service] = req.ReqID
		}
		ctx, cancel := context.WithCancel(context.Background())
		if b.runCancels == nil {
			b.runCancels = make(map[string]context.CancelFunc)
		}
		b.runCancels[req.ReqID] = cancel
		if req.Revision > b.revision {
			b.reqID, b.revision = req.ReqID, req.Revision
			b.text, b.sourceLang, b.targetLang = req.Text, req.SourceLang, req.TargetLang
			b.meta = nil
		}
		b.phase, b.message = "", ""
		if req.Service == "" {
			b.results = make(map[string]TranslationUpdateMsg)
		} else {
			delete(b.results, req.Service)
		}
		return ctx, true
	}
	return nil, false
}

func (m *bubbleManager) recordMeta(win *mygo.Window, meta RunMetaMsg, retry string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.byID {
		if b.win != win || b.runCancels[meta.ReqID] == nil {
			continue
		}
		if (retry != "" && b.serviceRequests[retry] == meta.ReqID) ||
			(retry == "" && b.fullReqID == meta.ReqID) {
			if b.serviceRequests == nil {
				b.serviceRequests = make(map[string]string)
			}
			if retry == "" {
				for _, service := range meta.Services {
					if _, retried := b.serviceRevisions[service]; !retried {
						b.serviceRequests[service] = meta.ReqID
					}
				}
			}
			if b.reqID == meta.ReqID {
				b.meta = &meta
				b.sourceLang, b.targetLang = meta.SourceLang, meta.TargetLang
			}
			return true
		}
	}
	return false
}

func (m *bubbleManager) recordResult(win *mygo.Window, msg TranslationUpdateMsg) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.byID {
		if b.win == win && b.serviceRequests[msg.Service] == msg.ReqID {
			if b.results == nil {
				b.results = make(map[string]TranslationUpdateMsg)
			}
			b.results[msg.Service] = msg
			return true
		}
	}
	return false
}

// Cancel advances the revision even if the earlier Run IPC has not arrived.
func (m *bubbleManager) Cancel(win *mygo.Window, session string, revision int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.byID {
		if b.win == win && b.sessionID == session && revision > b.revision {
			b.stopWork()
			b.revision = revision
			b.fullRevision, b.fullReqID = revision, ""
			b.reqID = ""
			b.serviceRequests = nil
			b.phase, b.message = "", ""
		}
	}
}

// completeCapture updates the existing progress bubble instead of reopening it.
func (m *bubbleManager) completeCapture(cfg config.Settings, shown Shown, text, message string) bool {
	m.mu.Lock()
	var state BubbleState
	found := false
	for _, b := range m.byID {
		if b.win == shown.Win && !b.closeClaimed && b.sessionID == shown.ReqID && b.revision == -1 {
			b.cancelCapture = nil
			b.advanceCaptureLayout()
			b.text, b.message, b.phase = text, message, ""
			if message != "" {
				b.phase = "error"
			}
			state = m.state(b, cfg)
			found = true
			break
		}
	}
	m.mu.Unlock()
	if found {
		BubbleReset.Emit(shown.Win, state)
	}
	return found
}

func cloneRequests(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (m *bubbleManager) finishRun(win *mygo.Window, reqID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.byID {
		if b.win == win {
			if cancel := b.runCancels[reqID]; cancel != nil {
				cancel()
				delete(b.runCancels, reqID)
			}
		}
	}
}

// stopPresentation ends user-visible ownership under mu. Internal layout hides
// do not call it; a dismissed page must not restart work through delayed IPC.
func (b *bubble) stopPresentation() RunStoppedMsg {
	b.stopWork()
	b.reqID = ""
	b.serviceRequests = nil
	b.revision++
	b.closeClaimed = true
	b.fullRevision, b.fullReqID = b.revision, ""
	b.canShow = func() bool { return false }
	if b.phase == "recognizing" {
		b.phase, b.message = "error", "已取消识别，可重新框选或直接输入。"
	}
	return RunStoppedMsg{SessionID: b.sessionID, ShowID: b.showID, Revision: b.revision}
}

// dismiss only applies to transient presentations; pinned jobs are independent.
func (b *bubble) dismiss() bool {
	if b.pinned || b.closeClaimed {
		return false
	}
	b.stopPresentation()
	return true
}

// present and claimClose are called under mu on the UI thread. Each explicit
// presentation owns its close, while a fresh session resets dismissed content.
func (b *bubble) present() {
	b.showID++
	b.presentationShown = false
	b.layoutWaitLogged = false
	b.closeClaimed = false
}

func (b *bubble) claimClose(session string, showID, revision int64) bool {
	if session != b.sessionID || showID != b.showID || b.closeClaimed || revision <= b.revision {
		return false
	}
	b.closeClaimed = true
	// OnClose advances once more, rejecting any Run IPC sent before Close.
	b.revision = revision
	return true
}

func (b *bubble) shouldHideAfterBlur(showID int64, visible, focused bool) bool {
	return b.showID == showID && !b.pinned && b.offBlur != nil && visible && !focused
}

// A recognition layout accepted before native readiness is still hidden. If
// OCR finishes then, require its replacement layout before the first show.
func (b *bubble) advanceCaptureLayout() {
	b.layoutID++
	if !b.presentationShown {
		b.awaitingLayout = true
	}
}
