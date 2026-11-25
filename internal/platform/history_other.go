//go:build !windows

package platform

// A nonactivating Mac panel releases keyboard focus when it closes.
func HistoryFocusReturn() func() { return nil }
