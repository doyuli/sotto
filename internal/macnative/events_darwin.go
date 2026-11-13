//go:build darwin

package macnative

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego/objc"
)

// Monitor is installed and removed on the main thread. Local callbacks must
// return the original event so the underlying app still receives its input.
func Monitor(mask uint64, local bool, observe func(objc.ID)) (func(), error) {
	var block objc.Block
	var token objc.ID
	Pool(func() {
		if local {
			block = objc.NewBlock(func(_ objc.Block, event objc.ID) objc.ID { observe(event); return event })
			token = Class("NSEvent").Send(Sel("addLocalMonitorForEventsMatchingMask:handler:"), mask, block)
		} else {
			block = objc.NewBlock(func(_ objc.Block, event objc.ID) { observe(event) })
			token = Class("NSEvent").Send(Sel("addGlobalMonitorForEventsMatchingMask:handler:"), mask, block)
		}
		token.Send(Sel("retain"))
	})
	if token == 0 {
		block.Release()
		return nil, fmt.Errorf("install native event monitor")
	}
	return sync.OnceFunc(func() {
		Pool(func() {
			Class("NSEvent").Send(Sel("removeMonitor:"), token)
			token.Send(Sel("release"))
			block.Release()
		})
	}), nil
}

func WindowContains(window, hit objc.ID) bool {
	for i := 0; hit != 0 && i < 16; i++ {
		if hit == window {
			return true
		}
		hit = hit.Send(Sel("parentWindow"))
	}
	return false
}

// Native NSMenu tracking windows are not attached as AppKit child windows.
// Treat a visible popup-menu-level window as an interaction inside our app.
func MenuWindow(window objc.ID) bool {
	return window != 0 && objc.Send[bool](window, Sel("isVisible")) && objc.Send[int64](window, Sel("level")) >= 101 // NSPopUpMenuWindowLevel
}
