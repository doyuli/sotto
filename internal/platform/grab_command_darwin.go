//go:build darwin

package platform

import (
	"context"

	"github.com/egoist/mygo"

	"sotto/internal/selection"
)

func runGrabCommand(ctx context.Context, path string) (result selection.Selection, err error) {
	// The clipboard fallback's NSEvent monitors require an AppKit run loop,
	// including in the --grab debugger. No windows or tray are created here.
	mygo.App.WhenReady(func() { go func() { result, err = grabCommandPath(ctx, path); mygo.App.Quit() }() })
	if runErr := mygo.App.Run(); runErr != nil {
		return selection.Selection{}, runErr
	}
	return result, err
}
