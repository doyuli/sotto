package app

import (
	"context"
	"sync"

	"sotto/internal/platform"
)

// entryGate gives each explicit entry action ownership of the transient UI.
// Pinned bubbles and the input draft retain their independent translation jobs.
type entryGate struct {
	mu      sync.Mutex
	current *entryTask
	next    int64
}
type entryTask struct {
	id        int64
	ctx       context.Context
	cancel    context.CancelFunc
	gate      *entryGate
	workspace uint64
}

func (g *entryGate) begin() *entryTask {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil {
		g.current.cancel()
	}
	g.next++
	ctx, cancel := context.WithCancel(context.Background())
	t := &entryTask{id: g.next, ctx: ctx, cancel: cancel, gate: g, workspace: platform.WorkspaceGeneration()}
	g.current = t
	return t
}
func (t *entryTask) Valid() bool {
	t.gate.mu.Lock()
	defer t.gate.mu.Unlock()
	return t.gate.current == t && t.ctx.Err() == nil && t.workspace == platform.WorkspaceGeneration()
}

func (g *entryGate) cancel() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil {
		g.current.cancel()
	}
}

// A workspace notification can arrive after a hotkey in the new workspace.
// Cancel only the old entry, never the user's newer explicit action.
func (g *entryGate) cancelStaleWorkspace() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil && g.current.workspace != platform.WorkspaceGeneration() {
		g.current.cancel()
	}
}

func (t *entryTask) ID() int64                { return t.id }
func (t *entryTask) Context() context.Context { return t.ctx }
func (t *entryTask) Cancel()                  { t.cancel() }
