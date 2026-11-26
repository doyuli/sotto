package selection

import (
	"context"
	"fmt"
	"log"
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
func (s *copyInputState) observeWindowsMouse(message uintptr) {
	const (
		wmMouseMove   = 0x0200
		wmMouseWheel  = 0x020A
		wmMouseHWheel = 0x020E
	)
	switch message {
	case wmMouseMove, wmMouseWheel, wmMouseHWheel:
		return
	default:
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
		cause = "mouse button event"
	case copyInputStopped:
		cause = "input watcher stopped"
	default:
		return nil
	}
	return fmt.Errorf("%s while %s: %w", cause, stage, ErrSourceChanged)
}

// Opening the clipboard for restoration may fail before its write guard ever runs.
func (s *copyInputState) finishAfterRestore(ctx context.Context, text string, restoreErr error, sourceForeground bool) (Selection, error) {
	if !sourceForeground {
		return Selection{}, fmt.Errorf("foreground after clipboard restore attempt: %w", ErrSourceChanged)
	}
	if err := s.check("finishing clipboard restoration"); err != nil {
		return Selection{}, err
	}
	return finishClipboardCopy(ctx, text, restoreErr)
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

// Windows sources can publish a single copy in several batches, even with no
// owner window. Wait for the last observed sequence to stay quiet before reading.
func waitForStableCopy(ctx context.Context, before uint32, sample func() (uint32, error)) (uint32, error) {
	const settle = 40 * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	started := time.Now()
	last, changedAt := before, started
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		seq, err := sample()
		if err != nil {
			return 0, err
		}
		if seq != last {
			last, changedAt = seq, time.Now()
			log.Printf("selection: clipboard copy=changed seq=%d wait=%s", seq, changedAt.Sub(started).Round(time.Millisecond))
		} else if seq != before && time.Since(changedAt) >= settle {
			return seq, ctx.Err()
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-tick.C:
		}
	}
}
