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
// Prepare captures identity on the hotkey thread, before background work queues.
func Prepare() func(context.Context) (Selection, error) {
	hwnd, pid, class := foregroundWindow()
	self := foregroundIsSelf()
	return func(ctx context.Context) (Selection, error) {
		if self {
			return Selection{}, ErrSelfWindow
		}
		if !sameForeground(hwnd, pid) {
			return Selection{}, ErrSourceChanged
		}
		sel, err := grabUIA(ctx)
		if ctx.Err() != nil {
			return Selection{}, ctx.Err()
		}
		if !sameForeground(hwnd, pid) {
			return Selection{}, ErrSourceChanged
		}
		if err == nil && sel.Text != "" {
			return sel, nil
		}
		return grabClipboardFrom(ctx, hwnd, pid, class)
	}
}
func grab(ctx context.Context) (Selection, error) { return Prepare()(ctx) }
func sameForeground(hwnd uintptr, pid uint32) bool {
	current, process, _ := foregroundWindow()
	return hwnd != 0 && current == hwnd && process == pid
}
