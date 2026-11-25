//go:build !windows

package platform

import "github.com/egoist/mygo"

func HistoryWindow(o mygo.WindowOptions) mygo.WindowOptions {
	return SettingsWindow(o)
}

// A nonactivating Mac panel releases keyboard focus when it closes.
func HistoryFocusReturn() func() { return nil }
