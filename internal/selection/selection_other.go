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

func startDoubleClick(onWord func()) error {
	return ErrUnsupported
}

func stopDoubleClick() {}

func WatchOutsideClick(hwnd uintptr, dismiss func()) (func(), error) {
	return nil, ErrUnsupported
}
