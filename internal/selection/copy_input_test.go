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

func TestWindowsScrollingAllowsCopyUntilMouseButtonInput(t *testing.T) {
	// Scrolling a document while waiting for the clipboard used to discard the selection.
	var s copyInputState
	for _, msg := range []uintptr{0x0200, 0x020A, 0x020E} { // move, vertical wheel, horizontal wheel
		s.observeWindowsMouse(msg)
	}
	if err := s.check("reading copy"); err != nil {
		t.Fatalf("movement or scrolling interrupted copy: %v", err)
	}
	s.observeWindowsMouse(0x0207) // Pressing the wheel is a middle-button click.
	s.observeWindowsMouse(0x020A)
	s.invalidate(copyInputStopped)
	if err := s.check("reading copy"); !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), "mouse button event") {
		t.Fatalf("expected the original button press to invalidate copy, got %v", err)
	}
}

func TestCopySourceAfterRestore(t *testing.T) {
	// A busy clipboard could bypass the write guard and publish text after a window switch.
	busy := errors.New("clipboard remained unavailable")
	for _, tc := range []struct {
		name       string
		foreground bool
		keyPressed bool
		restoreErr error
		wantErr    error
	}{
		{"busy but still current", true, false, busy, nil},
		{"switched windows while busy", false, false, busy, ErrSourceChanged},
		{"typed while busy", true, true, busy, ErrSourceChanged},
		{"switched windows during successful write", false, false, nil, ErrSourceChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var watch copyInputState
			if tc.keyPressed {
				watch.observeKey(false, 0)
			}
			got, err := watch.finishAfterRestore(t.Context(), "selected", tc.restoreErr, tc.foreground)
			want := Selection{}
			if tc.wantErr == nil {
				want = Selection{Text: "selected", Via: "clipboard"}
			}
			if got != want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("selection=%+v err=%v", got, err)
			}
		})
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
