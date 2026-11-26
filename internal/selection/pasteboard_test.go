package selection

import (
	"errors"
	"testing"
)

func TestPasteboardPreservesItemsAndEmptyFormats(t *testing.T) {
	types := [][]string{{"public.utf8-plain-text", "public.rtf"}, {"public.file-url", "app.custom-empty"}}
	items, err := snapshotPasteboard(types, func(i int, name string, limit int) ([]byte, error) {
		if name == "app.custom-empty" {
			return []byte{}, nil
		}
		return []byte(name), nil
	})
	if err != nil || len(items) != 2 || len(items[0]) != 2 || len(items[1]) != 2 || len(items[1][1].data) != 0 {
		t.Fatalf("snapshot=%v err=%v", items, err)
	}
}

func TestPasteboardAbortsForUnreadableRepresentations(t *testing.T) {
	want := errors.New("owner cannot provide data")
	items, err := snapshotPasteboard([][]string{{"public.utf8-plain-text", "promise"}}, func(int, string, int) ([]byte, error) { return nil, want })
	if items != nil || !errors.Is(err, want) {
		t.Fatalf("snapshot=%v err=%v", items, err)
	}
	if _, err := snapshotPasteboard([][]string{{"huge"}}, func(int, string, int) ([]byte, error) { return make([]byte, clipboardOtherLimit+1), nil }); err == nil {
		t.Fatal("accepted oversized private format")
	}
}

func TestPasteboardRestorePreservesContentsOnPreparationFailure(t *testing.T) {
	failure := errors.New("cannot prepare item")
	ops := pasteboardRestoreOps{
		prepare: func() error { return failure },
		clear:   func() int64 { t.Fatal("cleared clipboard after preparation failed"); return 0 },
		write:   func() error { t.Fatal("wrote after preparation failed"); return nil },
	}
	if err := restorePasteboardContents(10, ops); !errors.Is(err, failure) {
		t.Fatalf("preparation failure: %v", err)
	}
}

func TestPasteboardRestoreRejectsChangeDuringPreparation(t *testing.T) {
	version := int64(10)
	ops := pasteboardRestoreOps{
		prepare: func() error { version++; return nil },
		allowed: func() bool { return true },
		change:  func() int64 { return version },
		clear:   func() int64 { t.Fatal("cleared newer contents"); return 0 },
	}
	if err := restorePasteboardContents(10, ops); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("concurrent write: %v", err)
	}
}

func TestPasteboardRestoreReportsWriteFailure(t *testing.T) {
	failure := errors.New("cannot write items")
	version := int64(30)
	ops := pasteboardRestoreOps{
		prepare: func() error { return nil },
		allowed: func() bool { return true },
		change:  func() int64 { return version },
		clear:   func() int64 { version++; return version },
		write:   func() error { return failure },
	}
	if err := restorePasteboardContents(30, ops); !errors.Is(err, failure) {
		t.Fatalf("write failure: %v", err)
	}
}

func TestPasteboardRestoreRejectsChangeAfterClear(t *testing.T) {
	version := int64(40)
	ops := pasteboardRestoreOps{
		prepare: func() error { return nil },
		allowed: func() bool { return true },
		change:  func() int64 { return version },
		clear: func() int64 {
			version = 42 // Another app writes immediately after our clear (41).
			return 41
		},
		write: func() error { t.Fatal("overwrote concurrent copy"); return nil },
	}
	err := restorePasteboardContents(40, ops)
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("ownership change: %v", err)
	}
}

func TestPasteboardRestoreRejectsCanceledGuard(t *testing.T) {
	ops := pasteboardRestoreOps{
		prepare: func() error { return nil },
		allowed: func() bool { return false },
		clear:   func() int64 { t.Fatal("cleared after cancellation"); return 0 },
		write:   func() error { t.Fatal("wrote after cancellation"); return nil },
	}
	if err := restorePasteboardContents(50, ops); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("canceled guard: %v", err)
	}
}
