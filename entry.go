package main

import (
	"context"
	"sync"
)

// entryGate gives each explicit entry action ownership of the transient UI.
// Pinned bubbles and the input draft retain their independent translation jobs.
type entryGate struct {
	mu      sync.Mutex
	current *entryTask
	next    int64
}
type entryTask struct {
	id     int64
	ctx    context.Context
	cancel context.CancelFunc
	gate   *entryGate
}

func (g *entryGate) begin() *entryTask {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil {
		g.current.cancel()
	}
	g.next++
	ctx, cancel := context.WithCancel(context.Background())
	t := &entryTask{id: g.next, ctx: ctx, cancel: cancel, gate: g}
	g.current = t
	return t
}
func (t *entryTask) valid() bool {
	t.gate.mu.Lock()
	defer t.gate.mu.Unlock()
	return t.gate.current == t && t.ctx.Err() == nil
}

func (g *entryGate) cancel() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.current != nil {
		g.current.cancel()
	}
}
