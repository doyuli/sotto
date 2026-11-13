//go:build darwin

package app

import (
	"log"
	"os"
	"slices"

	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

func (a *application) platformMenuItems() []*mygo.MenuItem {
	openPermission := func(anchor string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			go func() {
				if err := mygo.Shell.OpenExternal("x-apple.systempreferences:com.apple.preference.security?" + anchor); err != nil {
					log.Printf("open permission settings: %v", err)
				}
			}()
		}
	}
	items := []*mygo.MenuItem{{Label: "系统权限", Submenu: []*mygo.MenuItem{
		{Label: "辅助功能（划词翻译）…", Click: openPermission("Privacy_Accessibility")},
		{Label: "屏幕录制（截图翻译）…", Click: openPermission("Privacy_ScreenCapture")},
	}}}
	return append(items, a.bubbles.HiddenPinnedMenuItems()...)
}

func (a *application) readyPlatform() {
	stop, err := macnative.WatchWorkspace(func() {
		a.entry.cancelStaleWorkspace()
		a.snip.HideStale()
		a.bubbles.HideStale()
		a.refreshTrayMenu()
	})
	if err != nil {
		log.Printf("watch workspace: %v", err)
		return
	}
	mygo.App.OnQuit(stop)
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		if slices.Contains(args, "--settings") {
			a.showSettings(false)
		} else {
			a.startEntry("panel")
		}
	})
	if slices.Contains(os.Args[1:], "--settings") {
		a.showSettings(false)
	} else if slices.Contains(os.Args[1:], "--panel") {
		a.startEntry("panel")
	}
}

func (a *application) startScreenshot(task *entryTask) {
	go func() {
		if err := macnative.RequireScreenRecording(true); err != nil {
			a.bubbles.ShowTask(a.settings(), "ocr", "", nil, "error", err.Error(), task.Valid, nil)
			return
		}
		mygo.RunOnMain(func() {
			if task.Valid() {
				a.snip.Show(task)
			}
		})
	}()
}
