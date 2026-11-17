//go:build !darwin

package platform

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

func InitUpdates(iconPNG []byte) {
	mygo.Use(native.New(updater.Options{Language: "zh-Hans", Icon: iconPNG, DisableAutomaticChecks: true}))
	if updater.AutomaticChecks() {
		updater.SetAutomaticChecks(false)
	}
}

func SetAutomaticUpdates(on bool)          { updater.SetAutomaticChecks(on) }
func UpdateMenuItem(func()) *mygo.MenuItem { return updater.MenuItem() }
