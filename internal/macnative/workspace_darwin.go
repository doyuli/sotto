//go:build darwin

package macnative

import (
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego/objc"
)

var workspaceGeneration atomic.Uint64

func WorkspaceGeneration() uint64 { return workspaceGeneration.Load() }

// WatchWorkspace is installed and removed on the main thread. The generation
// changes before cancellation so workers can reject results already queued.
func WatchWorkspace(changed func()) (func(), error) {
	if _, err := Framework("AppKit"); err != nil {
		return nil, err
	}
	var center objc.ID
	var tokens []objc.ID
	var blocks []objc.Block
	Pool(func() {
		center = Class("NSWorkspace").Send(Sel("sharedWorkspace")).Send(Sel("notificationCenter"))
		for _, name := range []string{"NSWorkspaceActiveSpaceDidChangeNotification", "NSWorkspaceWillSleepNotification", "NSWorkspaceScreensDidSleepNotification"} {
			block := objc.NewBlock(func(_ objc.Block, _ objc.ID) {
				workspaceGeneration.Add(1)
				PostMain(changed)
			})
			token := center.Send(Sel("addObserverForName:object:queue:usingBlock:"), String(name), objc.ID(0), objc.ID(0), block)
			token.Send(Sel("retain"))
			tokens, blocks = append(tokens, token), append(blocks, block)
		}
	})
	return sync.OnceFunc(func() {
		Pool(func() {
			for i, token := range tokens {
				center.Send(Sel("removeObserver:"), token)
				token.Send(Sel("release"))
				blocks[i].Release()
			}
		})
	}), nil
}

// NSRunningApplication snapshots may be queried off the main thread. Copy the
// PID out, never pass a borrowed Objective-C application to a worker.
func FrontmostPID() int32 {
	var pid int32
	Pool(func() {
		app := Class("NSWorkspace").Send(Sel("sharedWorkspace")).Send(Sel("frontmostApplication"))
		pid = objc.Send[int32](app, Sel("processIdentifier"))
	})
	return pid
}
