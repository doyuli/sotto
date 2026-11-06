//go:build !windows

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

func WatchOutsideClick(hwnd uintptr, dismiss func()) (func(), error) {
	return nil, ErrUnsupported
}

func Prepare() func(context.Context) (Selection, error) { return grab }

func RecoverClipboard(id string) error { return ErrUnsupported }
