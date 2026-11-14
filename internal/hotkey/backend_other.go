//go:build !darwin && !windows

package hotkey

import "fmt"

type systemBackend struct{}

func NewBackend() Backend { return systemBackend{} }
func (systemBackend) Register(string, func()) error {
	return fmt.Errorf("当前平台暂不支持全局快捷键")
}
func (systemBackend) Unregister(string) {}
func (systemBackend) Close()            {}
