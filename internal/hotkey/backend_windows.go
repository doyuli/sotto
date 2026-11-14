package hotkey

import (
	"fmt"
	"github.com/egoist/mygo"
)

type systemBackend struct{}

func NewBackend() Backend { return systemBackend{} }
func (systemBackend) Register(s string, fn func()) error {
	if err := mygo.GlobalShortcut.Register(s, fn); err != nil {
		return fmt.Errorf("系统无法注册该组合键，可能已被占用，请更换组合键：%w", err)
	}
	return nil
}
func (systemBackend) Unregister(s string) { mygo.GlobalShortcut.Unregister(s) }
func (systemBackend) Close()              { mygo.GlobalShortcut.UnregisterAll() }
