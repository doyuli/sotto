package app

import (
	"context"
	"sync"

	"github.com/egoist/mygo"

	"sotto/internal/platform"
	"sotto/internal/selection"
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
	minimal   bool
	cursor    mygo.Point
}

func (g *entryGate) begin(minimal bool) *entryTask {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil {
		g.current.cancel()
	}
	g.next++
	ctx, cancel := context.WithCancel(context.Background())
	t := &entryTask{id: g.next, ctx: ctx, cancel: cancel, gate: g, minimal: minimal, workspace: platform.WorkspaceGeneration()}
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

func (t *entryTask) source() mygo.Rectangle {
	return mygo.Rectangle{X: t.cursor.X, Y: t.cursor.Y}
}

// Clipboard grabs have no bounds. Keep both presentations at the pointer
// position captured with the shortcut, even if the mouse has since moved.
func (t *entryTask) selectionSource(sel selection.Selection) mygo.Rectangle {
	if sel.HasRect {
		return mygo.Rectangle{X: sel.Rect.X, Y: sel.Rect.Y, Width: sel.Rect.W, Height: sel.Rect.H}
	}
	return t.source()
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
