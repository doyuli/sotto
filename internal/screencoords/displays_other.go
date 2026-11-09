//go:build !windows

package screencoords

// Current has no Windows display layout on other platforms.
func Current() Layout { return NewLayout(nil, 96) }
