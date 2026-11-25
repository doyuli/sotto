//go:build !windows

package app

import "github.com/egoist/mygo"

func clearTrayPress() {}

func (a *application) trayClickBounds() mygo.Rectangle { return a.tray.Bounds() }

func (a *application) showTrayMenu() { a.trackTrayMenu() }
