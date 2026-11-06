//go:build windows

package selection

import (
	"context"
	"time"
)

const (
	clipboardPoll    = 10 * time.Millisecond
	clipboardSettle  = 40 * time.Millisecond
	clipboardTimeout = 2 * time.Second
	modifierWait     = 1200 * time.Millisecond
)

var clipboardMu = make(chan struct{}, 1)

func init() { clipboardMu <- struct{}{} }

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
	chord, ok := copyChordFor(class)
	if !ok {
		return Selection{}, ErrNoSelection
	}
	if err := waitCopyModifiers(ctx, modifierWait); err != nil {
		return Selection{}, err
	}
	if !sameForeground(hwnd, pid) {
		return Selection{}, ErrSourceChanged
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	saved := saveClipboard()
	if saved == nil {
		return Selection{}, ErrClipboardUnsafe
	}
	before := saved.sequence
	if clipboardSeq() != before || !sameForeground(hwnd, pid) {
		return Selection{}, ErrSourceChanged
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	if !sendCopy(chord) {
		return Selection{}, ErrNoSelection
	}
	inputStamp, ok := lastInputStamp()
	if !ok {
		return Selection{}, ErrClipboardUnsafe
	}
	deadline := time.NewTimer(clipboardTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(clipboardPoll)
	defer tick.Stop()
	for {
		stamp, known := lastInputStamp()
		if !known || stamp != inputStamp || !sameForeground(hwnd, pid) {
			return Selection{}, ErrSourceChanged
		}
		seq := clipboardSeq()
		if seq != before {
			if clipboardOwnerPID() != pid {
				return Selection{}, ErrSourceChanged
			}
			// Let multiple formats settle, but accept no new input in that interval.
			time.Sleep(clipboardSettle)
			seq = clipboardSeq()
			stamp, known = lastInputStamp()
			if !known || stamp != inputStamp || clipboardOwnerPID() != pid || !sameForeground(hwnd, pid) {
				return Selection{}, ErrSourceChanged
			}
			text, read := readClipboardText()
			stamp, known = lastInputStamp()
			if !known || stamp != inputStamp || clipboardSeq() != seq || clipboardOwnerPID() != pid {
				return Selection{}, ErrSourceChanged
			}
			// Compare again while holding the clipboard open; an intervening copy wins.
			restoreClipboardIfUnchanged(saved, seq, pid, inputStamp)
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
