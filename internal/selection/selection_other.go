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

func WatchWindowLeave(hwnd uintptr, changed func(primaryPress bool)) (func(), error) {
	return nil, ErrUnsupported
}

func HasPrimaryPress() bool { return false }

func WindowOwnsForeground(hwnd uintptr) bool { return false }

func Prepare() func(context.Context) (Selection, error) { return grab }

func RecoverClipboard(id string) error { return ErrUnsupported }
