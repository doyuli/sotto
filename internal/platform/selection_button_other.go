//go:build !windows && !darwin

package platform

import "github.com/egoist/mygo"

func ConfigureSelectionButtonWindow(*mygo.Window, func()) error { return nil }
