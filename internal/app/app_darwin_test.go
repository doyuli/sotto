//go:build darwin

package app

import (
	"errors"
	"fmt"
	"testing"

	"sotto/internal/config"
	"sotto/internal/macnative"
)

func TestScreenRecordingFailureMessage(t *testing.T) {
	// A component load error used to be displayed verbatim in both screenshot modes.
	denied := fmt.Errorf("check permission: %w", macnative.ErrScreenRecording)
	if got := screenRecordingFailureMessage(denied); got != macnative.ErrScreenRecording.Error() {
		t.Fatalf("permission message = %q", got)
	}
	loadErr := errors.New("resolve CGPreflightScreenCaptureAccess: symbol not found")
	if got := screenRecordingFailureMessage(loadErr); got != "无法初始化截图功能，请重启 Sotto 后重试" {
		t.Fatalf("component load message = %q", got)
	}
}

func TestWorkspaceCancellationPreservesNewEntry(t *testing.T) {
	var gate entryGate
	old := gate.begin()
	old.workspace++
	if old.Valid() {
		t.Fatal("old workspace entry stayed valid")
	}
	gate.cancelStaleWorkspace()
	if old.ctx.Err() == nil {
		t.Fatal("old workspace work was not canceled")
	}
	fresh := gate.begin()
	gate.cancelStaleWorkspace()
	if !fresh.Valid() {
		t.Fatal("delayed notification canceled a new workspace entry")
	}
	fresh.cancel()
}

func TestWorkspaceCancellationPreservesNewMinimalJob(t *testing.T) {
	// The watcher can run after a request has started in the new Space.
	a := &application{cfg: config.Settings{MinimalTranslation: true}}
	stale := a.entry.begin()
	staleCtx, ok := a.beginMinimalJob(stale)
	if !ok {
		t.Fatal("first job rejected")
	}
	a.minimalJobs[stale.id].workspace++
	fresh := a.entry.begin()
	freshCtx, ok := a.beginMinimalJob(fresh)
	if !ok {
		t.Fatal("new workspace job rejected")
	}
	a.cancelStaleMinimalJobs()
	if staleCtx.Err() == nil || a.finishMinimalJob(stale.id) || freshCtx.Err() != nil {
		t.Fatal("workspace change did not cancel only the stale job")
	}
	a.stopMinimalJobs()
}
