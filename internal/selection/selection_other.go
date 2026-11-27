//go:build !windows && !darwin

package selection

import "context"

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

func Prepare(_ string) func(context.Context) (Selection, error) { return grab }
