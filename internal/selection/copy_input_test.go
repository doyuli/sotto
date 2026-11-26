package selection

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestCopyDoesNotInvalidateItsOwnInjectedEvents(t *testing.T) {
	s := &copyInputState{marker: 123}
	// Ctrl down, C down/up, Ctrl up may arrive after SendInput returns.
	for range 4 {
		s.observeKey(true, 123)
		if !s.unchanged() {
			t.Fatal("copy canceled itself")
		}
	}
}

func TestUserInputStaysInvalidAfterLaterCopyEvents(t *testing.T) {
	for _, injected := range []bool{false, true} {
		s := &copyInputState{marker: 123}
		s.observeKey(true, 123)
		s.observeKey(injected, 456)
		s.observeKey(true, 123)
		if s.unchanged() {
			t.Fatal("copy events erased intervening user/other injected input")
		}
	}
}

func TestCopyMarkerMustMatchCurrentTransactionAndInjectedFlag(t *testing.T) {
	s := &copyInputState{marker: 124}
	s.observeKey(true, 123) // late event from an earlier copy
	if s.unchanged() {
		t.Fatal("accepted old transaction's input")
	}
	s = &copyInputState{marker: 124}
	s.observeKey(false, 124)
	if s.unchanged() {
		t.Fatal("ignored physical input with coincidentally matching extra info")
	}
}

func TestPointerMovementAllowsCopyUntilButtonOrWheelInput(t *testing.T) {
	s := &copyInputState{marker: 123}
	s.observeMouse(true)
	s.observeKey(true, 123)
	s.observeMouse(true)
	if err := s.check("settling copy formats"); err != nil {
		t.Fatalf("pointer movement alone interrupted copy: %v", err)
	}

	s.observeMouse(false)
	s.observeMouse(true)
	s.invalidate(copyInputStopped)
	err := s.check("settling copy formats")
	if !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), "mouse button or wheel event") {
		t.Fatalf("expected the original mouse input to invalidate copy, got %v", err)
	}
}

func TestCopyReleaseStopsOnTimeoutOrCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		held := func() bool { return true }
		if err := waitCopyRelease(t.Context(), held); !errors.Is(err, ErrShortcutHeld) {
			t.Fatalf("held input allowed copy: %v", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := waitCopyRelease(ctx, held); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled wait returned %v", err)
		}
	})
}

func TestCopyWaitsForLastFormatBatch(t *testing.T) {
	// A second sequence change used to reject an otherwise readable ownerless copy.
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		seq, err := waitForStableCopy(t.Context(), 100, func() (uint32, error) {
			switch elapsed := time.Since(started); {
			case elapsed < 10*time.Millisecond:
				return 100, nil
			case elapsed < 40*time.Millisecond:
				return 101, nil
			default:
				return 102, nil
			}
		})
		if err != nil || seq != 102 || time.Since(started) != 80*time.Millisecond {
			t.Fatalf("sequence=%d elapsed=%v err=%v", seq, time.Since(started), err)
		}
	})
}

func TestCopySettlingStopsOnTimeoutOrSourceChange(t *testing.T) {
	for _, test := range []struct {
		name    string
		sample  func(uint32) (uint32, error)
		wantErr error
	}{
		{"old clipboard", func(uint32) (uint32, error) { return 10, nil }, context.DeadlineExceeded},
		{"continuous changes", func(i uint32) (uint32, error) { return 10 + i, nil }, context.DeadlineExceeded},
		{"interrupted", func(i uint32) (uint32, error) {
			if i == 3 {
				return 0, ErrSourceChanged
			}
			return 11, nil
		}, ErrSourceChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls uint32
				started := time.Now()
				seq, err := waitForStableCopy(t.Context(), 10, func() (uint32, error) {
					calls++
					return test.sample(calls)
				})
				if seq != 0 || !errors.Is(err, test.wantErr) || time.Since(started) > time.Second {
					t.Fatalf("sequence=%d elapsed=%v err=%v", seq, time.Since(started), err)
				}
			})
		})
	}
}
