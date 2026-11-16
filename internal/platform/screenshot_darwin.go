//go:build darwin

package platform

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

const (
	nsNonactivatingPanelMask = 1 << 7 // NSWindowStyleMaskNonactivatingPanel
	nsWindowAnimationNone    = 2      // NSWindowAnimationBehaviorNone
	cgScreenSaverLevelKey    = 13     // kCGScreenSaverWindowLevelKey
)

var screenshotWindowLevel = sync.OnceValues(func() (int64, error) {
	var levelForKey func(int32) int32
	if err := macnative.Bind("CoreGraphics", macnative.Symbol{Target: &levelForKey, Name: "CGWindowLevelForKey"}); err != nil {
		return 0, err
	}
	return int64(levelForKey(cgScreenSaverLevelKey)), nil
})

// ScreenshotSource identifies the frontmost application while a snapshot is pending.
func ScreenshotSource() uintptr { return uintptr(macnative.FrontmostPID()) }

// ConfigureScreenshotWindow runs on the UI thread before showing the panel.
func ConfigureScreenshotWindow(w *mygo.Window, bounds mygo.Rectangle) error {
	level, err := screenshotWindowLevel()
	if err != nil {
		return err
	}
	panel := objc.ID(w.NativeHandle())
	if panel == 0 {
		return errors.New("screenshot panel has no native window")
	}
	// MyGo's Frameless option hides the title bar but keeps the titled style,
	// which AppKit can constrain below the menu bar. Borderless has no mask
	// bit. Keep the nonactivating bit that was set when the panel was created.
	panel.Send(macnative.Sel("setStyleMask:"), uint64(nsNonactivatingPanelMask))
	panel.Send(macnative.Sel("setLevel:"), level)
	panel.Send(macnative.Sel("setAnimationBehavior:"), int64(nsWindowAnimationNone))
	w.SetBounds(bounds)
	return nil
}
