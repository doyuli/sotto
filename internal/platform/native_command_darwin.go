//go:build darwin && e2e

package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/macnative"
)

// A local desktop probe for the actual window paths. No credentials, network
// services, clipboard writes or permission prompts are used by this command.
func HandleNativeCommand() bool {
	if len(os.Args) != 2 || os.Args[1] != "--native-smoke" {
		return false
	}
	report := struct {
		Accessory, Nonactivating, FullscreenAuxiliary            bool
		PassiveVisible, PassiveUnfocused, PassiveKeptApplication bool
		InputFocused, InputKeptApplication, SpaceUnchanged       bool
		MainQueueDispatched, ScreenshotCoversDisplay             bool
		Error                                                    string `json:"error,omitempty"`
	}{}
	var initialPID int32
	var initialGeneration uint64
	var focusObserved bool
	// Focus can arrive before the independently queued AppKit operation.
	// Keep the native loop running until both observations have completed.
	finish := func() {
		if focusObserved && report.MainQueueDispatched && report.ScreenshotCoversDisplay {
			mygo.App.Quit()
		}
	}
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		initial := macnative.FrontmostPID()
		initialPID = initial
		stop, err := macnative.WatchWorkspace(func() {})
		if err != nil {
			report.Error = err.Error()
			mygo.App.Quit()
			return
		}
		mygo.App.OnQuit(stop)
		generation := macnative.WorkspaceGeneration()
		initialGeneration = generation
		macnative.PostMain(func() {
			report.MainQueueDispatched = true
			finish()
		})
		w := mygo.NewWindow(SettingsWindow(mygo.WindowOptions{Title: "Sotto 原生验证", Hidden: true, Width: 360, Height: 180, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Sotto macOS 面板验证") })}))
		w.OnReadyToShow(func() {
			native := objc.ID(w.NativeHandle())
			app := macnative.Class("NSApplication").Send(macnative.Sel("sharedApplication"))
			report.Accessory = objc.Send[int64](app, macnative.Sel("activationPolicy")) == 1
			report.Nonactivating = objc.Send[uint64](native, macnative.Sel("styleMask"))&(1<<7) != 0
			report.FullscreenAuxiliary = objc.Send[uint64](native, macnative.Sel("collectionBehavior"))&(1<<8) != 0
			ShowBubbleWindow(w, "selection")
			waitNativeState(func() bool { return w.IsVisible() }, func() {
				report.PassiveVisible, report.PassiveUnfocused = w.IsVisible(), !w.IsFocused()
				report.PassiveKeptApplication = macnative.FrontmostPID() == initial
				w.OnFocus(func() {
					report.InputFocused = w.IsFocused()
					report.InputKeptApplication = macnative.FrontmostPID() == initial
					report.SpaceUnchanged = macnative.WorkspaceGeneration() == generation
					focusObserved = true
					w.Hide()
					macnative.PostMain(func() {
						probeScreenshotWindow(initial, func(err error) {
							if err != nil {
								report.Error = err.Error()
								mygo.App.Quit()
								return
							}
							report.ScreenshotCoversDisplay = true
							finish()
						})
					})
				})
				ShowBubbleWindow(w, "input")
			})
		})
	})
	timer := time.AfterFunc(10*time.Second, func() {
		mygo.RunOnMain(func() {
			report.InputKeptApplication = macnative.FrontmostPID() == initialPID
			report.SpaceUnchanged = macnative.WorkspaceGeneration() == initialGeneration
			report.Error = "native window or main-queue probe timed out"
			mygo.App.Quit()
		})
	})
	defer timer.Stop()
	if err := mygo.App.Run(); err != nil {
		report.Error = err.Error()
	}
	json.NewEncoder(os.Stdout).Encode(report)
	if report.Error != "" || !report.Accessory || !report.Nonactivating || !report.FullscreenAuxiliary || !report.PassiveVisible || !report.PassiveUnfocused || !report.PassiveKeptApplication || !report.InputFocused || !report.InputKeptApplication || !report.SpaceUnchanged || !report.MainQueueDispatched || !report.ScreenshotCoversDisplay {
		os.Exit(1)
	}
	return true
}

// A titled floating panel can land below the real menu bar and paint a second
// copy of the bar inside the snapshot. Check the actual window after ordering.
func probeScreenshotWindow(frontmost int32, done func(error)) {
	displays := mygo.Screen.Displays()
	if len(displays) == 0 {
		done(fmt.Errorf("no display for screenshot panel probe"))
		return
	}
	bounds := displays[0].Bounds
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 截图遮罩验证", Hidden: true, Frameless: true, Panel: true,
		SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true, Transparent: true,
		X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height,
		Content: ui.View(func(c *ui.Context) { ui.Box(c).Fill() }),
	})
	native := objc.ID(w.NativeHandle())
	content := native.Send(macnative.Sel("contentView"))
	if err := ConfigureScreenshotWindow(w, bounds); err != nil {
		done(fmt.Errorf("configure screenshot panel: %w", err))
		return
	}
	level, err := screenshotWindowLevel()
	if err != nil {
		done(err)
		return
	}
	w.OnFocus(func() {
		actual := w.ContentBounds()
		style := objc.Send[uint64](native, macnative.Sel("styleMask"))
		actualLevel := objc.Send[int64](native, macnative.Sel("level"))
		if actual != bounds || style != nsNonactivatingPanelMask || actualLevel != level {
			done(fmt.Errorf("shown screenshot panel: bounds=%+v want=%+v style=%#x level=%d want=%d", actual, bounds, style, actualLevel, level))
			return
		}
		if content == 0 || native.Send(macnative.Sel("contentView")) != content || !w.IsFocused() || macnative.FrontmostPID() != frontmost {
			done(fmt.Errorf("shown screenshot panel lost its content or nonactivating keyboard focus"))
			return
		}
		w.Hide()
		done(nil)
	})
	w.ShowInactive()
	w.Show()
}

// Poll only the visible-state boundary; input completion uses the focus event.
func waitNativeState(ready func() bool, next func()) {
	if ready() {
		next()
		return
	}
	time.AfterFunc(10*time.Millisecond, func() { mygo.RunOnMain(func() { waitNativeState(ready, next) }) })
}
