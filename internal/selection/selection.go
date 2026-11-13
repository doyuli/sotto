// Package selection reads the text selected in the foreground window of
// another app. It uses UI Automation on Windows or Accessibility on macOS,
// falling back to a guarded simulated copy with clipboard preservation.
package selection

import (
	"context"
	"errors"
)

// Rect is a rectangle in mygo's DIP screen coordinates.
type Rect struct{ X, Y, W, H int }

// Selection is what a grab found. Rect is the selection's on-screen
// bounds, when the path could see them, for placing the bubble.
type Selection struct {
	Text    string
	Rect    Rect
	HasRect bool
	// Via is the path that produced it: "uia", "ax", or "clipboard".
	Via string
}

// The errors Grab reports.
var (
	// ErrNoSelection: nothing was selected, or the foreground app
	// ignored the simulated copy.
	ErrNoSelection           = errors.New("no text selected")
	ErrSourceChanged         = errors.New("selection source changed")
	ErrShortcutHeld          = errors.New("keys or mouse buttons still held")
	ErrClipboardUnsafe       = errors.New("clipboard cannot be safely preserved")
	ErrClipboardRestore      = errors.New("clipboard restore incomplete")
	ErrClipboardRecoveryGone = errors.New("clipboard recovery no longer available")
	// ErrSelfWindow: the foreground window is the app's own, whose
	// selection is not what a hotkey means to grab.
	ErrSelfWindow = errors.New("the foreground window is the app's own")
	// ErrUnsupported: this platform has no selection grabbing.
	ErrUnsupported = errors.New("selection is not supported on this platform")
)

// Grab returns the selection of the foreground app: through UI
// Automation where it answers, else by simulating a copy. It stays
// within ctx's deadline.
func Grab(ctx context.Context) (Selection, error) {
	return grab(ctx)
}

// GrabUIA runs only the native text path (Windows UIA or macOS AX).
// An app without text support reports ErrNoSelection.
func GrabUIA(ctx context.Context) (Selection, error) {
	return grabUIA(ctx)
}

// GrabClipboard runs only the simulated-copy fallback, for the --grab
// debug command.
func GrabClipboard(ctx context.Context) (Selection, error) {
	return grabClipboard(ctx)
}
