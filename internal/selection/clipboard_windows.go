//go:build windows

package selection

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	clipboardPoll    = 10 * time.Millisecond
	clipboardSettle  = 40 * time.Millisecond
	clipboardTimeout = 2 * time.Second
	inputReleaseWait = 1200 * time.Millisecond
)

func grabClipboard(ctx context.Context) (Selection, error) {
	hwnd, pid, class := foregroundWindow()
	return grabClipboardFrom(ctx, hwnd, pid, class)
}

// A copy is a bounded transaction. Never undo a late write in the background:
// a later deliberate copy from the same process cannot be distinguished by PID.
func grabClipboardFrom(ctx context.Context, hwnd uintptr, pid uint32, class string) (sel Selection, err error) {
	started, stage := time.Now(), "wait_slot"
	defer func() {
		log.Printf("selection: clipboard stage=%s hwnd=%x pid=%d elapsed=%s err=%v", stage, hwnd, pid, time.Since(started).Round(time.Millisecond), err)
	}()
	select {
	case <-ctx.Done():
		return Selection{}, ctx.Err()
	case <-clipboardMu:
	}
	defer func() { clipboardMu <- struct{}{} }()
	stage = "check_recovery"
	if PendingClipboardRecovery() != "" {
		return Selection{}, ErrClipboardRestore
	}
	chord, ok := copyChordFor(class)
	if !ok {
		return Selection{}, ErrNoSelection
	}
	stage = "wait_input_release"
	if err := waitCopyRelease(ctx, inputReleaseWait); err != nil {
		return Selection{}, err
	}
	if !sameForeground(hwnd, pid) {
		return Selection{}, fmt.Errorf("foreground before clipboard snapshot: %w", ErrSourceChanged)
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	stage = "create_owner"
	owner, cleanup, err := clipboardOwner()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer cleanup()
	stage = "snapshot"
	saved, err := saveClipboard()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	stage = "prepare_restore"
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
	stage = "watch_input"
	watch, err := watchCopyInput()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer watch.stop()
	// Hook setup can take time; validate the original source again before copy.
	if copyControlsDown(keyDown) {
		return Selection{}, fmt.Errorf("key or button held before copy injection: %w", ErrSourceChanged)
	}
	if clipboardSeq() != before || !sameForeground(hwnd, pid) {
		return Selection{}, fmt.Errorf("clipboard or foreground before copy injection: %w", ErrSourceChanged)
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	if err := watch.check("preparing to send copy"); err != nil {
		return Selection{}, err
	}
	stage = "send_copy"
	if !sendCopy(chord, watch.marker) {
		return Selection{}, ErrNoSelection
	}
	deadline := time.NewTimer(clipboardTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(clipboardPoll)
	defer tick.Stop()
	stage = "wait_copy"
	for {
		if err := watch.check("waiting for copy"); err != nil {
			return Selection{}, err
		}
		if !sameForeground(hwnd, pid) {
			return Selection{}, fmt.Errorf("foreground while waiting for copy: %w", ErrSourceChanged)
		}
		seq := clipboardSeq()
		if seq != before {
			if clipboardOwnerPID() != pid {
				return Selection{}, fmt.Errorf("clipboard owner after copy: %w", ErrSourceChanged)
			}
			// Let multiple formats settle before reading and restoring.
			time.Sleep(clipboardSettle)
			seq = clipboardSeq()
			if err := watch.check("settling copy formats"); err != nil {
				return Selection{}, err
			}
			if clipboardOwnerPID() != pid || !sameForeground(hwnd, pid) {
				return Selection{}, fmt.Errorf("clipboard owner or foreground while settling copy formats: %w", ErrSourceChanged)
			}
			text, read := readClipboardText()
			if err := watch.check("reading copy"); err != nil {
				return Selection{}, err
			}
			if clipboardSeq() != seq || clipboardOwnerPID() != pid || !sameForeground(hwnd, pid) {
				return Selection{}, fmt.Errorf("clipboard or foreground while reading copy: %w", ErrSourceChanged)
			}
			// Compare again while holding the clipboard open; an intervening copy wins.
			stage = "restore"
			outcome, restoredSeq, restoreErr := prepared.restore(owner, seq, func() bool {
				return clipboardSeq() == seq && clipboardOwnerPID() == pid && sameForeground(hwnd, pid) && watch.unchanged()
			})
			if outcome == restoreFailed {
				retainClipboardRecovery(saved, restoredSeq)
				return Selection{Text: text, Via: "clipboard"}, fmt.Errorf("%w: %w", ErrClipboardRestore, restoreErr)
			}
			if outcome == restoreSkipped {
				if err := watch.check("restoring clipboard"); err != nil {
					return Selection{}, err
				}
				return Selection{}, fmt.Errorf("clipboard restore no longer owned: %w", ErrSourceChanged)
			}
			if err := ctx.Err(); err != nil {
				return Selection{}, err
			}
			if !read || text == "" {
				return Selection{}, ErrNoSelection
			}
			stage = "complete"
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
