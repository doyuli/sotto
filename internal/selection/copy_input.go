package selection

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

const (
	copyInputKey uint32 = iota + 1
	copyInputInjectedKey
	copyInputMouse
	copyInputStopped
)

// Each copy owns a different marker: even a late injected event from a previous
// copy invalidates this transaction. No key values or typed text are retained.
type copyInputState struct {
	marker uintptr
	cause  atomic.Uint32
}

func (s *copyInputState) observeKey(injected bool, marker uintptr) {
	if !injected {
		s.invalidate(copyInputKey)
	} else if marker != s.marker {
		s.invalidate(copyInputInjectedKey)
	}
}

// All buttons must be released before injection. A drag that starts
// during the copy will invalidate it on the button press.
func (s *copyInputState) observeMouse(moveOnly bool) {
	if !moveOnly {
		s.invalidate(copyInputMouse)
	}
}

func (s *copyInputState) invalidate(cause uint32) { s.cause.CompareAndSwap(0, cause) }
func (s *copyInputState) unchanged() bool         { return s.cause.Load() == 0 }

func (s *copyInputState) check(stage string) error {
	var cause string
	switch s.cause.Load() {
	case copyInputKey:
		cause = "physical key event"
	case copyInputInjectedKey:
		cause = "other injected key event"
	case copyInputMouse:
		cause = "mouse button or wheel event"
	case copyInputStopped:
		cause = "input watcher stopped"
	default:
		return nil
	}
	return fmt.Errorf("%s while %s: %w", cause, stage, ErrSourceChanged)
}

const inputReleaseWait = 1200 * time.Millisecond

// waitCopyRelease waits for the entire shortcut to come up on its own. A
// modifier can alter the copy chord, and a later release of the shortcut's
// main key looks like new input to the clipboard transaction's watcher.
// Also wait for mouse buttons so the copy cannot begin during a drag.
func waitCopyRelease(ctx context.Context, held func() bool) error {
	timer := time.NewTimer(inputReleaseWait)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for held() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return ErrShortcutHeld
		case <-tick.C:
		}
	}
	return ctx.Err()
}
