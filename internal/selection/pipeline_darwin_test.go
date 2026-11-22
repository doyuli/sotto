//go:build darwin

package selection

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
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
			case 57: // Caps Lock remains latched throughout.
				return true
			}
			return false
		}
		buttonState = func(int32, uint32) bool { return false }
		if err := waitCopyRelease(t.Context(), copyControlsHeld); err != nil || time.Since(start) < release {
			t.Fatalf("wait ended at %v with %v", time.Since(start), err)
		}
	})
}
