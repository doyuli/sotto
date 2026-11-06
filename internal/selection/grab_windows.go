//go:build windows

package selection

import "context"

// grab reads the foreground app's selection: UI Automation where the app
// answers with text — browsers, Office, Windows Terminal — and a
// simulated copy for the rest: Excel's grid, PDF readers, and the
// Chromium-based apps whose UIA answers empty even with a selection
// made. An empty UIA answer is not trusted: several of those apps expose
// a TextPattern that reports no selection while one exists. The copy is a
// no-op when there really is nothing selected; Windows Terminal is the
// exception, where even its copy chord interrupts an empty selection, so
// the copy is skipped there (see copyChordFor).
func grab(ctx context.Context) (Selection, error) {
	if foregroundIsSelf() {
		return Selection{}, ErrSelfWindow
	}
	sel, err := grabUIA(ctx)
	if err == nil && sel.Text != "" {
		return sel, nil
	}
	// No text support, a timeout, a broken COM, or an empty answer the
	// app may not have meant: the copy may still work.
	return grabClipboard(ctx)
}
