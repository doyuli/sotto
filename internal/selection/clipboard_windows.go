//go:build windows

package selection

import (
	"context"
	"sync/atomic"
	"time"
)

// The simulated-copy fallback, for the apps UI Automation cannot read:
// Excel's grid, PDF readers, half the Electron world. It saves the
// clipboard, sends Ctrl+C, reads the text and puts the clipboard back,
// keeping the whole exchange out of the Win+V history. The state
// machine's constants sit above, so they stay tunable in one place.

const (
	clipboardPoll    = 10 * time.Millisecond
	clipboardSettle  = 40 * time.Millisecond
	clipboardTimeout = 800 * time.Millisecond
	// A large selection can take longer than the first window to reach
	// the clipboard — Excel with a few thousand cells does — so the grab
	// keeps watching a while longer and takes the copy when it lands.
	lateCopyWindow = 1200 * time.Millisecond
	lateCopyWatch  = 3 * time.Second
	lateCopyPoll   = 50 * time.Millisecond
	// The user is usually still holding the hotkey's modifiers when the
	// action runs; the copy must not go out with Alt, Shift or Win still
	// down. The wait spans well over a second — a slow key release
	// beats a failed grab — and when the keys do not come up, the copy
	// is skipped rather than sent corrupted. Ctrl needs no waiting: a
	// copy sent while Ctrl is held is exactly the copy.
	modifierWait = 1200 * time.Millisecond
)

// clipboardMu serializes fallback grabs: two overlapping saves and
// restores would trample each other.
var clipboardMu = make(chan struct{}, 1)

// clipboardGen counts grabs. A late-copy watcher outlives its grab by
// seconds; the counter is how it tells a copy it should undo from one a
// newer grab is already reading.
var clipboardGen atomic.Uint64

func init() { clipboardMu <- struct{}{} }

// grabClipboard copies the selection of the foreground app and reads it
// back off the clipboard.
func grabClipboard(ctx context.Context) (Selection, error) {
	<-clipboardMu
	defer func() { clipboardMu <- struct{}{} }()

	// Whose late copy to watch for, if one arrives, and where the copy
	// must still happen: the user may switch away while the grab waits,
	// and a synthesized copy into whatever comes to front is not ours.
	fgHwnd, fgPID, fgClass := foregroundWindow()

	// Windows Terminal has no copy chord that is safe when nothing is
	// selected; its UIA path answers a selection, so skip the copy.
	chord, ok := copyChordFor(fgClass)
	if !ok {
		return Selection{}, ErrNoSelection
	}

	// Save what is there now, to put back after reading ours.
	saved := saveClipboard()
	before := clipboardSeq()

	// The user may still hold the hotkey's Alt while the handler runs;
	// wait for the harmful keys to release — synthesizing their keyups
	// activates the window menu, which then swallows the copy (see
	// waitCopyModifiers). Keys still held when the wait runs out would
	// send a corrupted chord: skip the copy instead.
	if waitCopyModifiers(modifierWait) {
		return Selection{}, ErrNoSelection
	}
	if cur, _, _ := foregroundWindow(); cur != fgHwnd {
		return Selection{}, ErrNoSelection
	}
	// Only now, with the copy actually going out, does this grab take over
	// from any watcher still waiting on the previous one. A grab that
	// returns above never wrote the clipboard and must not cancel a restore
	// it does not replace.
	gen := clipboardGen.Add(1)
	sendCopy(chord)

	// Wait for the copy to land: the sequence number moves the moment
	// anything writes, then settles while the app places any further
	// formats after it.
	changed := false
	deadline := time.Now().Add(clipboardTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		if clipboardSeq() != before {
			changed = true
			break
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(clipboardPoll)
	}
	if changed {
		time.Sleep(clipboardSettle)
	}

	if !changed {
		// The copy may simply be late (a big Excel selection). Keep
		// watching a short while; when the foreground app turns out to
		// be the one that wrote, that copy is ours: read it and return.
		late := time.Now().Add(lateCopyWindow)
		for time.Now().Before(late) {
			if err := ctx.Err(); err != nil {
				return Selection{}, err
			}
			time.Sleep(lateCopyPoll)
			if clipboardSeq() == before {
				continue
			}
			if pid := clipboardOwnerPID(); pid == 0 || pid != fgPID {
				// Someone else wrote: not ours to read.
				break
			}
			var text string
			if clipboardHasText() {
				text, _ = readClipboardText()
			}
			if saved != nil {
				restoreClipboard(saved)
			}
			if text != "" {
				return Selection{Text: text, Via: "clipboard"}, nil
			}
			return Selection{}, ErrNoSelection
		}
		// Nothing copied: leave the clipboard as it was — rewriting it
		// under our name would only make us its owner — but watch a
		// while for a copy arriving later still, and put the user's
		// clipboard back if it lands.
		if saved != nil {
			go undoLateCopy(saved, before, fgPID, gen)
		}
		return Selection{}, ErrNoSelection
	}

	var text string
	if clipboardHasText() {
		text, _ = readClipboardText()
	}
	if saved != nil {
		restoreClipboard(saved)
	}
	if text == "" {
		// The app copied something without text: a file, an image.
		return Selection{}, ErrNoSelection
	}
	return Selection{Text: text, Via: "clipboard"}, nil
}

// undoLateCopy watches for the target app's copy arriving after the
// timeout — Excel with a large selection does that — and puts the user's
// clipboard back. A deliberate copy the user makes from the same app
// within the window is indistinguishable from a late one; three seconds
// is the tradeoff that misses least in practice.
func undoLateCopy(saved *clipboardSnapshot, before uint32, fgPID uint32, gen uint64) {
	deadline := time.Now().Add(lateCopyWatch)
	for time.Now().Before(deadline) {
		time.Sleep(lateCopyPoll)
		if clipboardSeq() == before {
			continue
		}
		// If a newer grab owns the clipboard, defer to its own
		// save/restore so we don't replace the copy it's about to read.
		if clipboardGen.Load() != gen {
			return
		}
		// The target app's late copy gets the restore; anything else —
		// the user copying elsewhere — is none of ours.
		if pid := clipboardOwnerPID(); pid == 0 || pid != fgPID {
			return
		}
		// Serialize against in-flight grabs and re-verify: a grab that
		// started while we waited must not find its copy replaced.
		<-clipboardMu
		if cur := clipboardGen.Load(); cur == gen && clipboardSeq() != before {
			if pid := clipboardOwnerPID(); pid != 0 && pid == fgPID {
				restoreClipboard(saved)
			}
		}
		clipboardMu <- struct{}{}
		return
	}
}
