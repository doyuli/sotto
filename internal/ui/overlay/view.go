package overlay

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type resultView struct {
	text         string
	pending      bool
	animate      bool
	limit        size
	measured     size
	scroll       ui.ScrollState
	scrolledBack bool
	measure      func()
	dismiss      func()
	focus        func()
	cursor       func() mygo.Point
	position     func() mygo.Point
	move         func(mygo.Point)
	press        mygo.Point
	origin       mygo.Point
	dragging     bool
	pressed      bool
}

func (v *resultView) render(c *ui.Context) {
	background, ink := ui.Hex("#e0ded8"), ui.Hex("#252522")
	if c.Theme().Dark {
		background, ink = ui.Hex("#41413e"), ui.Hex("#f0efe9")
	}
	if !c.Preferences().ReduceTransparency {
		background = background.Alpha(0.87)
	}
	cursor := ui.CursorGrab
	if v.dragging {
		cursor = ui.CursorGrabbing
	}
	c.Root().Background(ui.Transparent)
	ui.Column(c).Fill().Radius(6).Clip().Background(background).Padding(paddingY, paddingX).
		Cursor(cursor).Focusable().HandleInput(v.input).Children(func() {
		if v.pending && v.text == "" {
			v.renderLoading(c, ink)
			return
		}
		boxWidth := v.limit.width
		if !v.pending {
			width, _ := c.MeasureText(0, ui.Span{Text: v.text, Size: 15})
			boxWidth = min(boxWidth, int(math.Ceil(float64(width)))+2*paddingX)
		}
		ui.Scroll(c).Fill().Scrollbars(false).TrackScroll(&v.scroll).Children(func() {
			// Measure at the result's width while its loading window is still narrow.
			ui.Text(c, v.text).Width(float32(max(1, boxWidth-2*paddingX))).Shrink(0).
				FontSize(15).FixedLineHeight(lineHeight).TextColor(ink).
				Draw(func(_ *ui.Painter, r ui.Rect) {
					if r.H > 0 {
						v.reportSize(boxWidth, int(math.Ceil(float64(r.H)))+2*paddingY)
					}
				})
		})
	})
}

func (v *resultView) setText(text string, pending bool) {
	// Follow new lines only for readers who are already at the end.
	if (v.pending || pending) && v.text != text && !v.scrolledBack && v.scroll.Y >= v.scroll.MaxY {
		v.scroll.Y = math.MaxFloat32
	}
	v.text, v.pending, v.animate = text, pending, false
}

func (v *resultView) renderLoading(c *ui.Context, ink ui.Color) {
	const label = "正在翻译"
	const fontSize, dotWidth, dotGap = 13, 18, 7
	width, _ := c.MeasureText(0, ui.Span{Text: label, Size: fontSize})
	v.reportSize(int(math.Ceil(float64(width)))+dotWidth+dotGap+2*paddingX, lineHeight+2*paddingY)
	animate := v.animate && !c.Preferences().ReduceMotion
	ui.Row(c).Height(lineHeight).Gap(dotGap).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(fontSize).TextColor(ink).SingleLine()
		ui.Box(c).Size(dotWidth, 4).Draw(func(p *ui.Painter, r ui.Rect) {
			phase := 0.0
			if animate {
				phase = float64(p.Now().UnixNano()%int64(time.Second)) / float64(time.Second)
				p.After(time.Second / 24)
			}
			for i := range 3 {
				alpha := float32(0.62)
				if animate {
					wave := math.Cos(2 * math.Pi * (phase - float64(i)/5))
					alpha = 0.58 + 0.24*float32(wave)
				}
				p.Fill(ui.Rect{X: r.X + float32(i*7), Y: r.Y, W: 4, H: 4}, ink.Alpha(alpha), 2)
			}
		})
	})
}

func (v *resultView) reportSize(width, height int) {
	measured := size{min(v.limit.width, width), min(v.limit.height, height)}
	if v.measured != measured {
		v.measured = measured
		v.measure()
	}
}

func (v *resultView) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputScroll:
		if v.pending && ev.DY != 0 && ev.Mods&ui.Shift == 0 && v.scroll.MaxY > 0 {
			// A mouse notch schedules motion before it updates the scroll offset.
			v.scrolledBack = ev.DY < 0
		}
	case ui.InputPointerDown:
		switch ev.Button {
		case 0:
			v.focus()
			v.press, v.origin = v.cursor(), v.position()
			v.pressed, v.dragging = true, false
			return true
		case 1:
			return true
		}
	case ui.InputPointerMove:
		if v.pressed {
			cursor := v.cursor()
			dx, dy := cursor.X-v.press.X, cursor.Y-v.press.Y
			v.dragging = v.dragging || abs(dx)+abs(dy) >= 4
			if v.dragging {
				v.move(mygo.Point{X: v.origin.X + dx, Y: v.origin.Y + dy})
			}
			return true
		}
	case ui.InputPointerUp:
		if ev.Button == 0 {
			v.pressed, v.dragging = false, false
			return true
		}
		if ev.Button == 1 && !ev.Canceled {
			v.dismiss()
			return true
		}
	case ui.InputKeyDown:
		if ev.Key == ui.KeyEscape && ev.Mods == 0 && !ev.Repeat {
			v.dismiss()
			return true
		}
	}
	return false
}

func abs(n int) int { return max(n, -n) }
