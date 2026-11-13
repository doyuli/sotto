//go:build !darwin

package app

import (
	"github.com/egoist/mygo"
)

func (a *application) platformMenuItems() []*mygo.MenuItem { return nil }
func (a *application) readyPlatform()                      {}
func (a *application) startScreenshot(task *entryTask)     { a.snip.Show(task) }
