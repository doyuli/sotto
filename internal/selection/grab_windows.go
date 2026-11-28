//go:build windows

package selection

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"
)

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
func Prepare(_ string, _ *mygo.Point) func(context.Context) (Selection, error) {
	hwnd, pid, class := foregroundWindow()
	read, check := prepareWindowsNativeRead(hwnd, pid, nil)
	return func(ctx context.Context) (Selection, error) {
		sel, err := read(ctx)
		if err == nil && sel.Text != "" {
			return sel, nil
		}
		if checkErr := check(ctx); checkErr != nil {
			return Selection{}, checkErr
		}
		if errors.Is(err, ErrSourceChanged) {
			return Selection{}, err
		}
		log.Printf("selection: clipboard fallback hwnd=%x pid=%d class=%q uia_error=%v", hwnd, pid, class, err)
		return grabClipboardFrom(ctx, hwnd, pid, class)
	}
}

func prepareWindowsNativeRead(hwnd uintptr, pid uint32, point *mygo.Point) (func(context.Context) (Selection, error), func(context.Context) error) {
	check := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if pid == windows.GetCurrentProcessId() {
			return ErrSelfWindow
		}
		if !sameForeground(hwnd, pid) {
			return ErrSourceChanged
		}
		return nil
	}
	read := func(ctx context.Context) (Selection, error) {
		if err := check(ctx); err != nil {
			return Selection{}, fmt.Errorf("foreground before UIA: %w", err)
		}
		sel, err := grabUIAAt(ctx, point, pid)
		if checkErr := check(ctx); checkErr != nil {
			return Selection{}, fmt.Errorf("foreground after UIA: %w", checkErr)
		}
		if errors.Is(err, errUIAMiss) {
			return Selection{}, ErrNoSelection
		}
		return sel, err
	}
	return read, check
}
func grab(ctx context.Context) (Selection, error) { return Prepare("", nil)(ctx) }

// PreparePointerSelection binds automatic capture and later validation to
// the source at mouse-up.
func PreparePointerSelection(point mygo.Point) (func(context.Context) (Selection, error), func(context.Context) error) {
	hwnd, pid, _ := foregroundWindow()
	return prepareWindowsNativeRead(hwnd, pid, &point)
}
func sameForeground(hwnd uintptr, pid uint32) bool {
	current, process, _ := foregroundWindow()
	return hwnd != 0 && current == hwnd && process == pid
}
