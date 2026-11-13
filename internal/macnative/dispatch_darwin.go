//go:build darwin

package macnative

import "github.com/ebitengine/purego/objc"

// PostMain does not wait for AppKit. In particular, a speech worker must be
// able to finish while the UI thread waits for Speaker.Close during quit.
func PostMain(fn func()) {
	Pool(func() {
		block := objc.NewBlock(func(_ objc.Block) { Pool(fn) })
		defer block.Release()
		queue := Class("NSOperationQueue").Send(Sel("mainQueue"))
		queue.Send(Sel("addOperationWithBlock:"), block)
	})
}
