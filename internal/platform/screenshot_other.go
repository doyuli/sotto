//go:build !darwin && !windows

package platform

func ScreenshotSource() uintptr { return 0 }
