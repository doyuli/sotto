//go:build darwin

package selection

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"sotto/internal/hotkey"
)

func TestMacSelectionFallbackAndPermission(t *testing.T) {
	denied := errors.New("permission denied")
	for _, tc := range []struct {
		text                   string
		readErr, permissionErr error
		copied                 bool
		wantErr                error
	}{
		{text: "selected"}, {readErr: ErrNoSelection, copied: true}, {copied: true}, {permissionErr: denied, wantErr: denied}, {readErr: ErrSourceChanged, wantErr: ErrSourceChanged},
	} {
		copied := false
		_, err := readMacSelection(context.Background(), func(ctx context.Context) error { return ctx.Err() }, func(bool) error { return tc.permissionErr }, func(context.Context) (Selection, error) { return Selection{Text: tc.text}, tc.readErr }, func(context.Context) (Selection, error) { copied = true; return Selection{Text: "copy"}, nil })
		if copied != tc.copied || !errors.Is(err, tc.wantErr) {
			t.Fatalf("%+v: copied=%t err=%v", tc, copied, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readMacSelection(ctx, func(ctx context.Context) error { return ctx.Err() }, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMacSelectionDropsTextWhenSourceChangesAfterAX(t *testing.T) {
	// Stale text could reach the error bubble alongside the source-change error.
	var sourceErr error
	sel, err := readMacSelection(t.Context(), func(context.Context) error { return sourceErr },
		func(bool) error { return nil },
		func(context.Context) (Selection, error) {
			sourceErr = ErrSourceChanged
			return Selection{Text: "previous window", HasRect: true}, nil
		}, func(context.Context) (Selection, error) {
			t.Fatal("sent copy to a different window")
			return Selection{}, nil
		})
	if sel != (Selection{}) || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("selection=%+v err=%v", sel, err)
	}
}

func TestMacCopyWaitsForLastShortcutKey(t *testing.T) {
	// Releasing Option before D used to cancel an already injected copy.
	originalKey, originalButton := keyState, buttonState
	t.Cleanup(func() { keyState, buttonState = originalKey, originalButton })
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		const release = 650 * time.Millisecond
		keyState = func(_ int32, key uint16) bool {
			switch key {
			case 58: // Option
				return time.Since(start) < 30*time.Millisecond
			case 2: // D
				return time.Since(start) < release
			case 57, 12: // Caps Lock and an unrelated key stay latched.
				return true
			}
			return false
		}
		buttonState = func(int32, uint32) bool { return false }
		mainKey := hotkey.MacMainKeyCode("Ctrl+Option+D")
		if err := waitCopyRelease(t.Context(), func() bool { return copyControlsHeld(mainKey) }); err != nil || time.Since(start) < release {
			t.Fatalf("wait ended at %v with %v", time.Since(start), err)
		}
	})
}

func TestMacCopyMonitorAllowsScrollAndWatchesButtons(t *testing.T) {
	// AppKit delivers wheel and trackpad scrolling as NSEventTypeScrollWheel.
	const scrollWheel = 22
	if macCopyInputMask&(1<<scrollWheel) != 0 {
		t.Fatal("scrolling would interrupt the copy")
	}
	for _, event := range []uint{1, 2, 3, 4, 25, 26} {
		if macCopyInputMask&(1<<event) == 0 {
			t.Fatalf("missing mouse button event %d", event)
		}
	}
}
