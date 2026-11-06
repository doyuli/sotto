package selection

import "testing"

func TestCopyDoesNotInvalidateItsOwnInjectedEvents(t *testing.T) {
	s := &copyInputState{marker: 123}
	// Ctrl down, C down/up, Ctrl up may arrive after SendInput returns.
	for range 4 {
		s.observe(true, 123)
		if !s.unchanged() {
			t.Fatal("copy canceled itself")
		}
	}
}

func TestUserInputStaysInvalidAfterLaterCopyEvents(t *testing.T) {
	for _, injected := range []bool{false, true} {
		s := &copyInputState{marker: 123}
		s.observe(true, 123)
		s.observe(injected, 456)
		s.observe(true, 123)
		if s.unchanged() {
			t.Fatal("copy events erased intervening user/other injected input")
		}
	}
}

func TestCopyMarkerMustMatchCurrentTransactionAndInjectedFlag(t *testing.T) {
	s := &copyInputState{marker: 124}
	s.observe(true, 123) // late event from an earlier copy
	if s.unchanged() {
		t.Fatal("accepted old transaction's input")
	}
	s = &copyInputState{marker: 124}
	s.observe(false, 124)
	if s.unchanged() {
		t.Fatal("ignored physical input with coincidentally matching extra info")
	}
}
