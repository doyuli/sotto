//go:build windows

package selection

import (
	"context"
	"fmt"
	"time"
)

const (
	clipboardPoll    = 10 * time.Millisecond
	clipboardSettle  = 40 * time.Millisecond
	clipboardTimeout = 2 * time.Second
	modifierWait     = 1200 * time.Millisecond
)

func grabClipboard(ctx context.Context) (Selection, error) {
	hwnd, pid, class := foregroundWindow()
	return grabClipboardFrom(ctx, hwnd, pid, class)
}

// A copy is a bounded transaction. Never undo a late write in the background:
// a later deliberate copy from the same process cannot be distinguished by PID.
func grabClipboardFrom(ctx context.Context, hwnd uintptr, pid uint32, class string) (Selection, error) {
	select {
	case <-ctx.Done():
		return Selection{}, ctx.Err()
	case <-clipboardMu:
	}
	defer func() { clipboardMu <- struct{}{} }()
	if PendingClipboardRecovery() != "" {
		return Selection{}, ErrClipboardRestore
	}
	chord, ok := copyChordFor(class)
	if !ok {
		return Selection{}, ErrNoSelection
	}
	if err := waitCopyModifiers(ctx, modifierWait); err != nil {
		return Selection{}, err
	}
	if !sameForeground(hwnd, pid) {
		return Selection{}, fmt.Errorf("foreground before clipboard snapshot: %w", ErrSourceChanged)
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	owner, cleanup, err := clipboardOwner()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer cleanup()
	saved := saveClipboard()
	if saved == nil {
		return Selection{}, ErrClipboardUnsafe
	}
	prepared, err := prepareClipboardRestore(saved)
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer prepared.free()
	before := saved.sequence
	if clipboardSeq() != before || !sameForeground(hwnd, pid) {
		return Selection{}, fmt.Errorf("clipboard or foreground during snapshot: %w", ErrSourceChanged)
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	watch, err := watchCopyInput()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer watch.stop()
	// Hook setup can take time; validate the original source again before copy.
	if !watch.unchanged() {
		return Selection{}, fmt.Errorf("input before copy injection: %w", ErrSourceChanged)
	}
	if clipboardSeq() != before || !sameForeground(hwnd, pid) {
		return Selection{}, fmt.Errorf("clipboard or foreground before copy injection: %w", ErrSourceChanged)
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	if !sendCopy(chord, watch.marker) {
		return Selection{}, ErrNoSelection
	}
	deadline := time.NewTimer(clipboardTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(clipboardPoll)
	defer tick.Stop()
	for {
		if !watch.unchanged() {
			return Selection{}, fmt.Errorf("input while waiting for copy: %w", ErrSourceChanged)
		}
		if !sameForeground(hwnd, pid) {
			return Selection{}, fmt.Errorf("foreground while waiting for copy: %w", ErrSourceChanged)
		}
		seq := clipboardSeq()
		if seq != before {
			if clipboardOwnerPID() != pid {
				return Selection{}, fmt.Errorf("clipboard owner after copy: %w", ErrSourceChanged)
			}
			// Let multiple formats settle, but accept no new input in that interval.
			time.Sleep(clipboardSettle)
			seq = clipboardSeq()
			if !watch.unchanged() {
				return Selection{}, fmt.Errorf("input while settling copy formats: %w", ErrSourceChanged)
			}
			if clipboardOwnerPID() != pid || !sameForeground(hwnd, pid) {
				return Selection{}, fmt.Errorf("clipboard owner or foreground while settling copy formats: %w", ErrSourceChanged)
			}
			text, read := readClipboardText()
			if !watch.unchanged() || clipboardSeq() != seq || clipboardOwnerPID() != pid {
				return Selection{}, fmt.Errorf("input or clipboard while reading copy: %w", ErrSourceChanged)
			}
			// Compare again while holding the clipboard open; an intervening copy wins.
			outcome, restoredSeq, restoreErr := prepared.restore(owner, seq, func() bool {
				return watch.unchanged() && clipboardSeq() == seq && clipboardOwnerPID() == pid
			})
			if outcome == restoreFailed {
				retainClipboardRecovery(saved, restoredSeq)
				return Selection{Text: text, Via: "clipboard"}, fmt.Errorf("%w: %w", ErrClipboardRestore, restoreErr)
			}
			if outcome == restoreSkipped {
				return Selection{}, fmt.Errorf("clipboard restore no longer owned: %w", ErrSourceChanged)
			}
			if err := ctx.Err(); err != nil {
				return Selection{}, err
			}
			if !read || text == "" {
				return Selection{}, ErrNoSelection
			}
			return Selection{Text: text, Via: "clipboard"}, nil
		}
		select {
		case <-ctx.Done():
			return Selection{}, ctx.Err()
		case <-deadline.C:
			return Selection{}, ErrNoSelection
		case <-tick.C:
		}
	}
}
