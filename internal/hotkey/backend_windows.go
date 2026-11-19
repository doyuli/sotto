package hotkey

import (
	"fmt"

	"github.com/egoist/mygo"
)

type systemBackend struct{}

func NewBackend() Backend { return systemBackend{} }
func (systemBackend) Register(s string, fn func()) error {
	if err := mygo.GlobalShortcut.Register(s, fn); err != nil {
		return fmt.Errorf("%w: %w", ErrSystemRegistration, err)
	}
	return nil
}
func (b systemBackend) RegisterEscape(fn func()) (func(), error) {
	if err := b.Register("Escape", fn); err != nil {
		return nil, err
	}
	return func() { b.Unregister("Escape") }, nil
}
func (systemBackend) Unregister(s string) { mygo.GlobalShortcut.Unregister(s) }
func (systemBackend) Close()              { mygo.GlobalShortcut.UnregisterAll() }
