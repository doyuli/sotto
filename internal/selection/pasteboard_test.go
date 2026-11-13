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

// A preparation failure has not touched the pasteboard. Even if another app
// writes during preparation, a retry must keep the original expected version.
func TestPasteboardFailedPreparationDoesNotClaimNewContents(t *testing.T) {
	version, text := int64(10), "original copy"
	failure := errors.New("cannot prepare item")
	ops := pasteboardRestoreOps{
		prepare: func() error { version, text = 11, "new user copy"; return failure },
		allowed: func() bool { return true },
		change:  func() int64 { return version },
		clear:   func() int64 { t.Fatal("cleared newer contents"); return 0 },
		write:   func() error { t.Fatal("overwrote newer contents"); return nil },
	}
	ok, retry, err := restorePasteboardContents(10, ops)
	if ok || retry != 10 || !errors.Is(err, failure) {
		t.Fatalf("preparation failure: restored=%t retry=%d err=%v", ok, retry, err)
	}
	ops.prepare = func() error { return nil }
	ok, retry, err = restorePasteboardContents(retry, ops)
	if ok || retry != -1 || !errors.Is(err, ErrSourceChanged) || text != "new user copy" {
		t.Fatalf("retry: restored=%t retry=%d text=%q err=%v", ok, retry, text, err)
	}
}

func TestPasteboardFailedWriteDoesNotClaimNewContents(t *testing.T) {
	for _, duringWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "between attempts", true: "during failed write"}[duringWrite], func(t *testing.T) {
			version, text := int64(20), "temporary selection"
			failure := errors.New("cannot write items")
			clears := 0
			ops := pasteboardRestoreOps{
				prepare: func() error { return nil },
				allowed: func() bool { return true },
				change:  func() int64 { return version },
				clear: func() int64 {
					clears++
					version++
					text = ""
					return version
				},
				write: func() error {
					if duringWrite {
						version, text = 22, "new user copy"
					}
					return failure
				},
			}
			ok, retry, err := restorePasteboardContents(20, ops)
			if ok || retry != 21 || !errors.Is(err, failure) {
				t.Fatalf("write failure: restored=%t retry=%d err=%v", ok, retry, err)
			}
			if !duringWrite {
				version, text = 22, "new user copy"
			}
			ops.write = func() error { t.Fatal("overwrote newer contents"); return nil }
			ok, retry, err = restorePasteboardContents(retry, ops)
			if ok || retry != -1 || !errors.Is(err, ErrSourceChanged) || clears != 1 || text != "new user copy" {
				t.Fatalf("retry: restored=%t retry=%d clears=%d text=%q err=%v", ok, retry, clears, text, err)
			}
		})
	}
}

func TestPasteboardFailedWriteCanRetryOwnClear(t *testing.T) {
	version, text := int64(30), "temporary selection"
	failure := errors.New("cannot write items")
	ops := pasteboardRestoreOps{
		prepare: func() error { return nil },
		allowed: func() bool { return true },
		change:  func() int64 { return version },
		clear:   func() int64 { version++; text = ""; return version },
		write:   func() error { return failure },
	}
	ok, retry, err := restorePasteboardContents(30, ops)
	if ok || !errors.Is(err, failure) {
		t.Fatalf("expected write failure: restored=%t err=%v", ok, err)
	}
	ops.write = func() error { version++; text = "original backup"; return nil }
	ok, _, err = restorePasteboardContents(retry, ops)
	if !ok || err != nil || text != "original backup" {
		t.Fatalf("retry on own clear: restored=%t text=%q err=%v", ok, text, err)
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
	ok, retry, err := restorePasteboardContents(40, ops)
	if ok || retry != -1 || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("ownership change: restored=%t retry=%d err=%v", ok, retry, err)
	}
}

func TestPasteboardRestoreRejectsUnknownOwnershipAndCanceledGuard(t *testing.T) {
	// Unknown ownership must not even materialize native replacement objects.
	if ok, retry, err := restorePasteboardContents(-1, pasteboardRestoreOps{}); ok || retry != -1 || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unknown ownership: restored=%t retry=%d err=%v", ok, retry, err)
	}
	ops := pasteboardRestoreOps{
		prepare: func() error { return nil },
		allowed: func() bool { return false },
		clear:   func() int64 { t.Fatal("cleared after cancellation"); return 0 },
		write:   func() error { t.Fatal("wrote after cancellation"); return nil },
	}
	if ok, retry, err := restorePasteboardContents(50, ops); ok || retry != -1 || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("canceled guard: restored=%t retry=%d err=%v", ok, retry, err)
	}
}
