package shared

import "github.com/egoist/mygo/ui"

var (
	brandBubbleSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><rect x="2.5" y="2.8" width="19" height="16.8" rx="5.2" fill="currentColor"/><path d="M7 19.2 5.8 22.4 9 20.6" fill="currentColor"/></svg>`))
	brandLetterSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 24 24"><path d="M15 8.2C14.3 7 12.4 6.8 11.25 7.5C9.84 8.34 9.6 9.73 10.9 10.6C12.07 11.39 13.6 11.39 14.3 12.2C15.23 13.24 14.65 15.23 13 15.8C11.72 16.3 10.31 15.8 9.49 14.88" fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" stroke-linejoin="round"/></svg>`))
)

func BrandMark(c *ui.Context, accent, background ui.Color) {
	ui.Box(c).Size(20, 20).Children(func() {
		ui.Icon(c, brandBubbleSVG).Size(20, 20).TextColor(accent)
		ui.Box(c).Absolute().Top(0).Left(0).Size(20, 20).Children(func() {
			ui.Icon(c, brandLetterSVG).Size(20, 20).TextColor(background)
		})
	})
}
