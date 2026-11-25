package app

import (
	"log"

	"github.com/egoist/mygo"

	"sotto/internal/config"
	"sotto/internal/platform"
	"sotto/internal/selection"
)

func clearTrayPress() { selection.ClearPrimaryMousePress() }

func (a *application) trayClickBounds() mygo.Rectangle { return selection.CursorDragBounds() }

func (a *application) showTrayMenu() {
	a.toggleTrayMenu(func() {
		stop, err := selection.WatchPrimaryMousePresses()
		if err != nil {
			log.Printf("watch tray menu mouse input: %v", err)
		} else {
			defer stop()
		}
		a.trackTrayMenu()
		// An outside mouse-down can end the popup before the tray gets mouse-up.
		a.recordTrayAutoDismiss(config.TrayActionMenu, selection.CapturePrimaryPress())
	}, func() {
		// MyGo checks for an installed menu after the right-click callback.
		a.tray.SetMenu(nil)
		platform.CloseTrayMenu()
	})
}
