// Package selection reads the text selected in the foreground window of
// another app. It uses UI Automation on Windows or Accessibility on macOS,
// falling back to a guarded simulated copy with clipboard preservation.
package selection

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
)

// Rect is a rectangle in mygo's DIP screen coordinates.
type Rect struct{ X, Y, W, H int }

// PrimaryPress captures a left press before a window-leave callback is queued.
// The point is in physical pixels on Windows and MyGo screen DIPs on macOS.
// A zero time means there was no primary press.
type PrimaryPress struct {
	At    time.Time
	Point mygo.Point
}

type SelectionPointerKind uint8

const (
	SelectionPointerCancel SelectionPointerKind = iota
	SelectionPointerDown
	SelectionPointerUp
)

// SelectionPointer reports an input edge without consuming the original event.
// Point uses the same platform coordinates as PrimaryPress.
type SelectionPointer struct {
	Kind  SelectionPointerKind
	At    time.Time
	Point mygo.Point
}

// WindowHasFocus includes owned popups that can hold focus after the window
// itself blurs. Call on the UI thread when handling a queued dismissal.
func WindowHasFocus(w *mygo.Window) bool {
	return w.IsFocused() || WindowOwnsForeground(w.NativeHandle())
}

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
	ErrNoSelection     = errors.New("no text selected")
	ErrSourceChanged   = errors.New("selection source changed")
	ErrShortcutHeld    = errors.New("keys or mouse buttons still held")
	ErrClipboardUnsafe = errors.New("clipboard cannot be safely preserved")
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

// The mouse can select text in web content while keyboard focus stays in
// another control. A leaf hit by the pointer may delegate selection to its document.
func readSelectionAncestors[T comparable](ctx context.Context, element T, read func(context.Context, T) (Selection, error), parent func(T) T, release func(T)) (Selection, error) {
	var zero T
	const maxAncestors = 24
	for depth := 0; element != zero && depth < maxAncestors; depth++ {
		if err := ctx.Err(); err != nil {
			release(element)
			return Selection{}, err
		}
		sel, err := read(ctx, element)
		if !errors.Is(err, ErrNoSelection) {
			release(element)
			return sel, err
		}
		var next T
		if depth+1 < maxAncestors && ctx.Err() == nil {
			next = parent(element)
		}
		release(element)
		element = next
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	return Selection{}, ErrNoSelection
}
