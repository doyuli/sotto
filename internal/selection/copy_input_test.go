package selection

import (
	"errors"
	"strings"
	"testing"
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
