package app

import (
	"errors"
	"maps"
	"testing"
)

func TestHotkeyCommitKeepsOldBindingOnFailure(t *testing.T) {
	for _, diskFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "registration", true: "persistence"}[diskFailure], func(t *testing.T) {
			old := map[string]string{"panel": "Ctrl+A", "ocr": "Ctrl+S"}
			desired := map[string]string{"panel": "Ctrl+B", "ocr": "Ctrl+T"}
			held := map[string]bool{"Ctrl+A": true, "Ctrl+S": true}
			published := false
			failure := errors.New("failed")
			err := commitHotkeys(old, desired, func(s string) error {
				if !held["Ctrl+A"] || !held["Ctrl+S"] {
					t.Fatal("released working shortcuts before commit")
				}
				if !diskFailure && s == "Ctrl+T" {
					return failure
				}
				held[s] = true
				return nil
			}, func(s string) { delete(held, s) }, func() error {
				if diskFailure {
					return failure
				}
				t.Fatal("persisted a failed registration")
				return nil
			}, func(map[string]string) { published = true })
			if !errors.Is(err, failure) || published || len(held) != 2 || !held["Ctrl+A"] || !held["Ctrl+S"] {
				t.Fatalf("error=%v held=%v published=%v", err, held, published)
			}
		})
	}
}

func TestHotkeyCommitSwapsWithoutReregistering(t *testing.T) {
	old := map[string]string{"panel": "Ctrl+A", "ocr": "Ctrl+S"}
	desired := map[string]string{"panel": "Ctrl+S", "ocr": "Ctrl+A"}
	var active map[string]string
	persisted := false
	err := commitHotkeys(old, desired, func(string) error { t.Fatal("re-registered an existing binding"); return nil }, func(string) { t.Fatal("released a reused binding") }, func() error { persisted = true; return nil }, func(next map[string]string) {
		if !persisted {
			t.Fatal("published before persistence")
		}
		active = next
	})
	if err != nil || !maps.Equal(active, desired) {
		t.Fatalf("active=%v err=%v", active, err)
	}
}

func TestHotkeyCommitReleasesOnlyObsoleteBindingsAfterPublish(t *testing.T) {
	old := map[string]string{"panel": "Ctrl+A", "ocr": "Ctrl+S"}
	desired := map[string]string{"panel": "Ctrl+B", "ocr": "Ctrl+S"}
	published := false
	var registered, removed string
	err := commitHotkeys(old, desired, func(s string) error { registered = s; return nil }, func(s string) {
		if !published {
			t.Fatal("released before publish")
		}
		removed = s
	}, func() error { return nil }, func(map[string]string) { published = true })
	if err != nil || registered != "Ctrl+B" || removed != "Ctrl+A" {
		t.Fatalf("registered=%s removed=%s err=%v", registered, removed, err)
	}
}
