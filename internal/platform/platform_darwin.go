//go:build darwin

package platform

import (
	_ "embed"
	"errors"
	"time"

	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

func WorkspaceGeneration() uint64 { return macnative.WorkspaceGeneration() }
func CaptureDelay() time.Duration { return 0 }

// Keep the menu bar silhouette independent of the app icon's colors and tile.
//
//go:embed assets/tray-template.png
var trayTemplatePNG []byte

func TrayOptions(o mygo.TrayOptions) mygo.TrayOptions {
	o.ID, o.IconIsTemplate, o.Icon = "main", true, trayTemplatePNG
	return o
}

func SelectionPermissionMessage(err error) string {
	if errors.Is(err, macnative.ErrAccessibility) {
		return macnative.ErrAccessibility.Error()
	}
	return ""
}

func Init() {
	// Set before Run: the first NSApplication initialization must be accessory.
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
}

func SettingsWindow(o mygo.WindowOptions) mygo.WindowOptions {
	o.Panel, o.Hidden = true, true
	return o
}

func ShowBubbleWindow(w *mygo.Window, trigger string) {
	if trigger == "input" {
		w.Show()
	} else {
		w.ShowInactive()
	}
}

func UpdateDescription() string {
	return "后台检查新版本，有更新时从菜单栏确认安装"
}
