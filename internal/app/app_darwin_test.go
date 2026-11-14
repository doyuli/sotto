//go:build darwin

package app

import "testing"

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
