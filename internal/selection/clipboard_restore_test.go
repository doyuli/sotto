package selection

import (
	"context"
	"errors"
	"testing"
)

func TestRestoreDoesNotTouchChangedClipboard(t *testing.T) {
	err := restoreClipboardFormats(6, 3, func() bool { return false },
		func() error { t.Fatal("cleared newer clipboard"); return nil },
		func(int) error { t.Fatal("overwrote newer clipboard"); return nil })
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unexpected restore error: %v", err)
	}
}

func TestRestoreEmptyFailureDoesNotWrite(t *testing.T) {
	failure := errors.New("empty failed")
	err := restoreClipboardFormats(6, 3, func() bool { return true },
		func() error { return failure },
		func(int) error { t.Fatal("wrote after empty failed"); return nil })
	if !errors.Is(err, failure) {
		t.Fatalf("unexpected restore error: %v", err)
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

func TestPayloadFailurePreservesOtherFormats(t *testing.T) {
	calls := make([]int, 6)
	clears := 0
	err := restoreClipboardFormats(6, 3, func() bool { return true }, func() error { clears++; return nil }, func(i int) error {
		calls[i]++
		if i == 4 {
			return errors.New("format failure")
		}
		return nil
	})
	if clears != 1 || err == nil || calls[4] != 3 || calls[5] != 1 {
		t.Fatal("partial failure was hidden or lost remaining formats")
	}
}

// A stable selection used to be lost whenever writing back its backup failed.
func TestFinishClipboardCopy(t *testing.T) {
	writeFailure := errors.New("write failed")
	for _, tc := range []struct {
		name, text          string
		restoreErr, wantErr error
	}{
		{"ordinary copy", "selected", nil, nil},
		{"write failed after reading", "selected", writeFailure, nil},
		{"empty copy", "", nil, ErrNoSelection},
		{"empty and write failed", "", writeFailure, writeFailure},
		{"source changed", "stale", ErrSourceChanged, ErrSourceChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := finishClipboardCopy(t.Context(), tc.text, tc.restoreErr)
			if !errors.Is(err, tc.wantErr) || (err != nil && got.Text != "") ||
				(err == nil && (got.Text != tc.text || got.Via != "clipboard")) {
				t.Errorf("selection=%+v err=%v", got, err)
			}
		})
	}
}

// Cancellation during write-back must also discard text when the write itself failed.
func TestCanceledCopyDiscardsTextOnRestoreFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := finishClipboardCopy(ctx, "stale", errors.New("write failed"))
	if got.Text != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("selection=%+v err=%v", got, err)
	}
}
