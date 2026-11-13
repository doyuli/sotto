//go:build darwin && e2e

package platform

import (
	"encoding/json"
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
		MainQueueDispatched                                      bool
		Error                                                    string `json:"error,omitempty"`
	}{}
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		initial := macnative.FrontmostPID()
		stop, err := macnative.WatchWorkspace(func() {})
		if err != nil {
			report.Error = err.Error()
			mygo.App.Quit()
			return
		}
		mygo.App.OnQuit(stop)
		generation := macnative.WorkspaceGeneration()
		macnative.PostMain(func() { report.MainQueueDispatched = true })
		w := mygo.NewWindow(SettingsWindow(mygo.WindowOptions{Title: "Sotto 原生验证", Hidden: true, Width: 360, Height: 180, Content: ui.View(func(c *ui.Context) { ui.Text(c, "Sotto macOS 面板验证") })}))
		w.OnReadyToShow(func() {
			native := objc.ID(w.NativeHandle())
			app := macnative.Class("NSApplication").Send(macnative.Sel("sharedApplication"))
			report.Accessory = objc.Send[int64](app, macnative.Sel("activationPolicy")) == 1
			report.Nonactivating = objc.Send[uint64](native, macnative.Sel("styleMask"))&(1<<7) != 0
			report.FullscreenAuxiliary = objc.Send[uint64](native, macnative.Sel("collectionBehavior"))&(1<<8) != 0
			ShowBubbleWindow(w, "selection")
			time.AfterFunc(100*time.Millisecond, func() {
				mygo.RunOnMain(func() {
					report.PassiveVisible, report.PassiveUnfocused = w.IsVisible(), !w.IsFocused()
					report.PassiveKeptApplication = macnative.FrontmostPID() == initial
					ShowBubbleWindow(w, "input")
					time.AfterFunc(100*time.Millisecond, func() {
						mygo.RunOnMain(func() {
							report.InputFocused = w.IsFocused()
							report.InputKeptApplication = macnative.FrontmostPID() == initial
							report.SpaceUnchanged = macnative.WorkspaceGeneration() == generation
							w.Hide()
							mygo.App.Quit()
						})
					})
				})
			})
		})
	})
	timer := time.AfterFunc(10*time.Second, func() { mygo.RunOnMain(func() { report.Error = "native readiness timed out"; mygo.App.Quit() }) })
	defer timer.Stop()
	if err := mygo.App.Run(); err != nil {
		report.Error = err.Error()
	}
	json.NewEncoder(os.Stdout).Encode(report)
	if report.Error != "" || !report.Accessory || !report.Nonactivating || !report.FullscreenAuxiliary || !report.PassiveVisible || !report.PassiveUnfocused || !report.PassiveKeptApplication || !report.InputFocused || !report.InputKeptApplication || !report.SpaceUnchanged || !report.MainQueueDispatched {
		os.Exit(1)
	}
	return true
}
