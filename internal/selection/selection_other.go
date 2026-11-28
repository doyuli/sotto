//go:build !windows && !darwin

package selection

import (
	"context"

	"github.com/egoist/mygo"
)

func grab(ctx context.Context) (Selection, error) {
	return Selection{}, ErrUnsupported
}

func grabUIA(ctx context.Context) (Selection, error) {
	return Selection{}, ErrUnsupported
}

func grabClipboard(ctx context.Context) (Selection, error) {
	return Selection{}, ErrUnsupported
}

func WatchWindowLeave(hwnd uintptr, changed func(PrimaryPress)) (func(), error) {
	return nil, ErrUnsupported
}

func CapturePrimaryPress() PrimaryPress { return PrimaryPress{} }

func WindowOwnsForeground(hwnd uintptr) bool { return false }

func Prepare(_ string, _ *mygo.Point) func(context.Context) (Selection, error) { return grab }

func PreparePointerSelection(_ mygo.Point) (func(context.Context) (Selection, error), func(context.Context) error) {
	return grab, func(context.Context) error { return ErrUnsupported }
}

func SelectionPointerScreenPoint(point mygo.Point) mygo.Point { return point }

func WatchSelectionPointer(func(SelectionPointer)) (func(), error) { return nil, ErrUnsupported }
