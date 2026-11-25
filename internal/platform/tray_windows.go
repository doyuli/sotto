package platform

import "golang.org/x/sys/windows"

var endTrayMenu = windows.NewLazySystemDLL("user32.dll").NewProc("EndMenu")

// CloseTrayMenu must run on the UI thread that is tracking the tray menu.
func CloseTrayMenu() { endTrayMenu.Call() }
