//go:build darwin

package macnative

import (
	"fmt"
	"os"
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
	var active atomic.Bool
	active.Store(true)
	Pool(func() {
		center = Class("NSWorkspace").Send(Sel("sharedWorkspace")).Send(Sel("notificationCenter"))
		for _, name := range []string{"NSWorkspaceActiveSpaceDidChangeNotification", "NSWorkspaceWillSleepNotification", "NSWorkspaceScreensDidSleepNotification"} {
			block := objc.NewBlock(func(_ objc.Block, _ objc.ID) {
				workspaceGeneration.Add(1)
				PostMain(func() {
					if active.Load() {
						changed()
					}
				})
			})
			token := center.Send(Sel("addObserverForName:object:queue:usingBlock:"), String(name), objc.ID(0), objc.ID(0), block)
			token.Send(Sel("retain"))
			tokens, blocks = append(tokens, token), append(blocks, block)
		}
	})
	return sync.OnceFunc(func() {
		active.Store(false)
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

// WatchActivation belongs to one presentation. Ignore notifications from an
// older switch if a new presentation already belongs to the active app.
func WatchActivation(changed func()) (func(), error) {
	if _, err := Framework("AppKit"); err != nil {
		return nil, err
	}
	source := FrontmostPID()
	var center, token objc.ID
	var block objc.Block
	var active atomic.Bool
	active.Store(true)
	Pool(func() {
		center = Class("NSWorkspace").Send(Sel("sharedWorkspace")).Send(Sel("notificationCenter"))
		block = objc.NewBlock(func(_ objc.Block, notification objc.ID) {
			app := notification.Send(Sel("userInfo")).Send(Sel("objectForKey:"), String("NSWorkspaceApplicationKey"))
			target := objc.Send[int32](app, Sel("processIdentifier"))
			if active.Load() && activationChanged(source, target, FrontmostPID(), int32(os.Getpid())) {
				changed()
			}
		})
		queue := Class("NSOperationQueue").Send(Sel("mainQueue"))
		token = center.Send(Sel("addObserverForName:object:queue:usingBlock:"), String("NSWorkspaceDidActivateApplicationNotification"), objc.ID(0), queue, block)
		token.Send(Sel("retain"))
	})
	if token == 0 {
		block.Release()
		return nil, fmt.Errorf("observe application activation")
	}
	return sync.OnceFunc(func() {
		active.Store(false)
		Pool(func() { center.Send(Sel("removeObserver:"), token); token.Send(Sel("release")); block.Release() })
	}), nil
}
