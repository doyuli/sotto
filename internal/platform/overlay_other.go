//go:build !windows

package platform

import "github.com/egoist/mygo"

// ConfigureOverlayWindow configures platform-specific frame attributes for a
// transparent overlay window.
func ConfigureOverlayWindow(*mygo.Window) {}
