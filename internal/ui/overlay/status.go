package overlay

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/platform"
)

// ShowStatus presents brief feedback without taking focus from the source app.
// It runs on the main thread, like the rest of the overlay manager.
func (m *Manager) ShowStatus(message string) {
	if m.suspended {
		// Keep the status out of both the capture and the snipping UI.
		m.pendingStatus = message
		return
	}
	if m.status != nil {
		m.status.Close()
	}
	work := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	if work.Width <= 0 || work.Height <= 0 {
		return
	}
	width, height := min(300, work.Width), min(56, work.Height)
	bounds := mygo.Rectangle{
		X: work.X + (work.Width-width)/2, Y: work.Y + max(0, work.Height-height-24),
		Width: width, Height: height,
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: "Sotto 状态", Content: ui.View(func(c *ui.Context) {
			renderStatus(c, message)
		}),
		X: bounds.X, Y: bounds.Y, Width: width, Height: height,
		Hidden: true, Frameless: true, Transparent: true, Panel: true,
		SkipTaskbar: true, AlwaysOnTop: true, DisableResize: true,
		DisableMaximize: true, DisableFullScreen: true, DisableShadow: true, IgnoreMouseEvents: true,
	})
	platform.ConfigureOverlayWindow(w)
	if w.Bounds() != bounds {
		w.SetBounds(bounds)
	}
	w.SetVisibleOnAllWorkspaces(false)
	m.status = w
	var timer *time.Timer
	w.OnReadyToShow(func() {
		w.ShowInactive()
		timer = time.AfterFunc(2*time.Second, w.Close)
	})
	w.OnClosed(func() {
		if timer != nil {
			timer.Stop()
		}
		if m.status == w {
			m.status = nil
		}
	})
}

func renderStatus(c *ui.Context, message string) {
	background, ink := ui.Hex("#ffffff"), ui.Hex("#202020")
	if c.Theme().Dark {
		background, ink = ui.Hex("#222325"), ui.Hex("#f3f4f5")
	}
	width, _ := c.Size()
	c.Root().Background(ui.Transparent)
	ui.Column(c).Fill().Center().Children(func() {
		ui.Column(c).MaxWidth(width).Padding(8, 14).Radius(12).Background(background).Children(func() {
			ui.Text(c, message).FontSize(13).TextColor(ink).TextAlign(ui.Center)
		})
	})
}
