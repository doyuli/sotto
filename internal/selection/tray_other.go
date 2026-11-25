//go:build !windows

package selection

import (
	"time"

	"github.com/egoist/mygo"
)

func WatchPrimaryMousePresses() (func(), error)      { return nil, ErrUnsupported }
func LastPrimaryMousePress() (time.Time, mygo.Point) { return time.Time{}, mygo.Point{} }
func ClearPrimaryMousePress()                        {}
func CursorDragBounds() mygo.Rectangle               { return mygo.Rectangle{} }
