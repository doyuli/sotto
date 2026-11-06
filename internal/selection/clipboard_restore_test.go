package selection

import (
	"errors"
	"testing"
)

func TestRestoreDoesNotTouchChangedClipboard(t *testing.T) {
	outcome, err := restoreClipboardFormats(6, 3, func() bool { return false },
		func() error { t.Fatal("cleared newer clipboard"); return nil },
		func(int) error { t.Fatal("overwrote newer clipboard"); return nil })
	if outcome != restoreSkipped || err != nil {
		t.Fatalf("outcome=%v err=%v", outcome, err)
	}
}

func TestRestoreEmptyFailureDoesNotWrite(t *testing.T) {
	failure := errors.New("empty failed")
	outcome, err := restoreClipboardFormats(6, 3, func() bool { return true },
		func() error { return failure },
		func(int) error { t.Fatal("wrote after empty failed"); return nil })
	if outcome != restoreFailed || !errors.Is(err, failure) {
		t.Fatalf("outcome=%v err=%v", outcome, err)
	}
}

func TestRestoreRetriesOnlyFailedFormats(t *testing.T) {
	for failed := 0; failed < 6; failed++ {
		calls := make([]int, 6)
		err := writeClipboardFormats(6, 3, func(i int) error {
			calls[i]++
			if i == failed && calls[i] < 3 {
				return errors.New("transient write failure")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for i, count := range calls {
			want := 1
			if i == failed {
				want = 3
			}
			if count != want {
				t.Fatalf("format %d: calls=%d want=%d", i, count, want)
			}
		}
	}
}

func TestProtectionFailureNeverWritesPayload(t *testing.T) {
	for failed := 0; failed < 3; failed++ {
		calls := make([]int, 6)
		failure := errors.New("protection failed")
		err := writeClipboardFormats(6, 3, func(i int) error {
			calls[i]++
			if i == failed {
				return failure
			}
			return nil
		})
		if !errors.Is(err, failure) || calls[failed] != 3 {
			t.Fatal("failure not surfaced or retries unbounded")
		}
		for i := failed + 1; i < len(calls); i++ {
			if calls[i] != 0 {
				t.Fatal("wrote beyond failed protection marker")
			}
		}
	}
}

func TestPayloadFailurePreservesOtherFormatsAndRecovery(t *testing.T) {
	calls := make([]int, 6)
	clears := 0
	outcome, err := restoreClipboardFormats(6, 3, func() bool { return true }, func() error { clears++; return nil }, func(i int) error {
		calls[i]++
		if i == 4 {
			return errors.New("format failure")
		}
		return nil
	})
	if outcome != restoreFailed || clears != 1 || err == nil || calls[4] != 3 || calls[5] != 1 {
		t.Fatal("partial failure was hidden or lost remaining formats")
	}
	snap := &clipboardSnapshot{formats: []clipboardFormat{{id: 13, data: []byte("original")}}}
	retainClipboardRecovery(snap, 42)
	id := PendingClipboardRecovery()
	defer DiscardClipboardRecovery(id)
	DiscardClipboardRecovery("stale")
	if PendingClipboardRecovery() != id {
		t.Fatal("stale discard removed recovery")
	}
	clipboardRecovery.Lock()
	kept := clipboardRecovery.snapshot == snap && clipboardRecovery.sequence == 42
	clipboardRecovery.Unlock()
	if !kept {
		t.Fatal("lost original snapshot")
	}
	DiscardClipboardRecovery(id)
	if PendingClipboardRecovery() != "" {
		t.Fatal("explicit discard retained snapshot")
	}
}
