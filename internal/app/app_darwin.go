//go:build darwin

package app

import (
	"errors"
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
		{Label: "辅助功能", Click: openPermission("Privacy_Accessibility")},
		{Label: "屏幕录制", Click: openPermission("Privacy_ScreenCapture")},
	}}}
	if item := a.bubbles.PinnedRestoreMenuItem(a.refreshTrayMenu); item != nil {
		items = append(items, item)
	}
	return items
}

func (a *application) readyPlatform() {
	stop, err := macnative.WatchWorkspace(func() {
		a.entry.cancelStaleWorkspace()
		a.cancelStaleMinimalJobs()
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
			a.showSettings(false, nil)
		} else {
			a.startEntry("panel")
		}
	})
	if slices.Contains(os.Args[1:], "--settings") {
		a.showSettings(false, nil)
	} else if slices.Contains(os.Args[1:], "--panel") {
		a.startEntry("panel")
	}
}

func (a *application) startScreenshot(task *entryTask) {
	go func() {
		if err := macnative.RequireScreenRecording(true); err != nil {
			message := screenRecordingFailureMessage(err)
			if task.minimal {
				a.showMinimalEntryError(task, task.source(), message)
				return
			}
			a.bubbles.ShowTask(a.settings(), "ocr", "", nil, "error", message, task.Valid, nil)
			return
		}
		mygo.RunOnMain(func() {
			if task.Valid() {
				a.snip.Show(task)
			}
		})
	}()
}

func screenRecordingFailureMessage(err error) string {
	if errors.Is(err, macnative.ErrScreenRecording) {
		return macnative.ErrScreenRecording.Error()
	}
	log.Printf("initialize screen recording permissions: %v", err)
	return "无法初始化截图功能，请重启 Sotto 后重试"
}
