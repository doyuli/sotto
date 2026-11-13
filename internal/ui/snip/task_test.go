package snip

import "context"

type testGate struct {
	current *testTask
	next    int64
}
type testTask struct {
	id     int64
	ctx    context.Context
	cancel context.CancelFunc
	gate   *testGate
}

func (g *testGate) Begin() *testTask {
	if g.current != nil {
		g.current.Cancel()
	}
	g.next++
	ctx, cancel := context.WithCancel(context.Background())
	t := &testTask{id: g.next, ctx: ctx, cancel: cancel, gate: g}
	g.current = t
	return t
}
func (t *testTask) ID() int64                { return t.id }
func (t *testTask) Context() context.Context { return t.ctx }
func (t *testTask) Cancel()                  { t.cancel() }
func (t *testTask) Valid() bool              { return t.gate.current == t && t.ctx.Err() == nil }
