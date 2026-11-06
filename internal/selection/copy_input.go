package selection

import "sync/atomic"

// Each copy owns a different marker: even a late injected event from a previous
// copy invalidates this transaction. No key values or typed text are retained.
type copyInputState struct {
	marker  uintptr
	changed atomic.Bool
}

func (s *copyInputState) observe(injected bool, marker uintptr) {
	if !injected || marker != s.marker {
		s.changed.Store(true)
	}
}

func (s *copyInputState) unchanged() bool { return !s.changed.Load() }
