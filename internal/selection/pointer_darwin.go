//go:build darwin

package selection

import (
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

// WatchSelectionPointer runs on the main thread so NSEvent monitor lifetimes
// stay on the AppKit thread. The receiver only queues input.
func WatchSelectionPointer(receive func(SelectionPointer)) (func(), error) {
	const (
		leftDown  = 1
		leftUp    = 2
		rightDown = 3
		keyDown   = 10
		scroll    = 22
		otherDown = 25
		mask      = 1<<leftDown | 1<<leftUp | 1<<rightDown | 1<<keyDown | 1<<scroll | 1<<otherDown
	)
	observe := func(event objc.ID) {
		kind := event.Send(macnative.Sel("type"))
		if kind == keyDown {
			receive(SelectionPointer{})
			return
		}
		pointer := SelectionPointer{At: time.Now(), Point: mygo.Screen.CursorScreenPoint()}
		if event.Send(macnative.Sel("modifierFlags"))&macControlModifier == 0 {
			switch kind {
			case leftDown:
				pointer.Kind = SelectionPointerDown
			case leftUp:
				pointer.Kind = SelectionPointerUp
			}
		}
		receive(pointer)
	}
	global, err := macnative.Monitor(mask, false, observe)
	if err != nil {
		return nil, err
	}
	local, err := macnative.Monitor(mask, true, observe)
	if err != nil {
		global()
		return nil, err
	}
	activation, err := macnative.WatchForegroundChanges(func() { receive(SelectionPointer{}) })
	if err != nil {
		local()
		global()
		return nil, err
	}
	return func() { activation(); local(); global() }, nil
}

func SelectionPointerScreenPoint(point mygo.Point) mygo.Point { return point }
