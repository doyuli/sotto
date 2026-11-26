//go:build windows

package selection

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"time"
)

const (
	clipboardPoll    = 10 * time.Millisecond
	clipboardSettle  = 40 * time.Millisecond
	clipboardTimeout = time.Second
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
	chord, ok := copyChordFor(class)
	if !ok {
		return Selection{}, ErrNoSelection
	}
	stage = "wait_input_release"
	if err := waitCopyRelease(ctx, func() bool { return copyControlsDown(keyDown) }); err != nil {
		return Selection{}, err
	}
	if !sameForeground(hwnd, pid) {
		return Selection{}, fmt.Errorf("foreground before clipboard snapshot: %w", ErrSourceChanged)
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

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	stage = "snapshot"
	saved, before, err := saveClipboard()
	if errors.Is(err, ErrSourceChanged) {
		return Selection{}, err
	}
	var prepared *preparedClipboard
	if err == nil {
		prepared, err = prepareClipboardRestore(saved)
	}
	if err != nil {
		log.Printf("selection: clipboard copy without restore: %v", err)
	} else {
		defer prepared.free()
	}
	// A delayed clipboard provider can take time. Recheck the source before injecting copy.
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
	sent := time.Now()
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
			log.Printf("selection: clipboard copy=changed wait=%s", time.Since(sent).Round(time.Millisecond))
			if _, err := checkClipboardOwner(pid); err != nil {
				return Selection{}, err
			}
			// Let all formats settle, including copies initially published without an owner.
			select {
			case <-ctx.Done():
				return Selection{}, ctx.Err()
			case <-time.After(clipboardSettle):
			}
			settled := clipboardSeq()
			if err := watch.check("settling copy formats"); err != nil {
				return Selection{}, err
			}
			if !sameForeground(hwnd, pid) {
				return Selection{}, fmt.Errorf("foreground while settling: %w", ErrSourceChanged)
			}
			owned, err := checkClipboardOwner(pid)
			if err != nil {
				return Selection{}, err
			}
			if settled != seq && !owned {
				return Selection{}, fmt.Errorf("ownerless clipboard changed while settling: %w", ErrSourceChanged)
			}
			seq = settled
			text, _ := readClipboardText()
			if err := watch.check("reading copy"); err != nil {
				return Selection{}, err
			}
			if clipboardSeq() != seq || !sameForeground(hwnd, pid) {
				return Selection{}, fmt.Errorf("clipboard or foreground while reading: %w", ErrSourceChanged)
			}
			stillOwned, err := checkClipboardOwner(pid)
			if err != nil {
				return Selection{}, err
			}
			if !owned || !stillOwned || prepared == nil {
				stage = "read_without_restore"
				return finishClipboardCopy(text, ctx.Err())
			}
			stage = "restore"
			owner, cleanup, err := clipboardOwner()
			if err != nil {
				retainClipboardRecovery(saved, seq)
				return finishClipboardCopy(text, fmt.Errorf("%w: %w", ErrClipboardRestore, err))
			}
			defer cleanup()
			outcome, restoredSeq, err := prepared.restore(owner, seq, func() bool {
				owned, _ := checkClipboardOwner(pid)
				return owned && clipboardSeq() == seq && sameForeground(hwnd, pid) && watch.unchanged()
			})
			switch outcome {
			case restoreFailed:
				retainClipboardRecovery(saved, restoredSeq)
				return finishClipboardCopy(text, fmt.Errorf("%w: %w", ErrClipboardRestore, err))
			case restoreSkipped:
				return Selection{}, ErrSourceChanged
			}
			return finishClipboardCopy(text, ctx.Err())
		}
		select {
		case <-ctx.Done():
			return Selection{}, ctx.Err()
		case <-deadline.C:
			log.Printf("selection: clipboard copy=timeout wait=%s", time.Since(sent).Round(time.Millisecond))
			return Selection{}, context.DeadlineExceeded
		case <-tick.C:
		}
	}
}
