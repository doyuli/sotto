package shared

import "github.com/egoist/mygo/ui"

type ConfirmDialogColors struct {
	Dark   bool
	Panel  ui.Color
	Border ui.Color
	Text   ui.Color
	Muted  ui.Color
}

type ConfirmDialogOptions struct {
	Title        string
	Message      string
	CancelLabel  string
	ConfirmLabel string
	Colors       ConfirmDialogColors
	OnConfirm    func()
	OnCancel     func()
}

// ConfirmDialog renders a centered confirmation modal with tuned backdrop
// contrast, card framing, and content hierarchy using native theme buttons.
func ConfirmDialog(c *ui.Context, open *bool, opts ConfirmDialogOptions) ui.Element {
	if open == nil || !*open {
		return ui.Element{}
	}
	colors := opts.Colors
	w, h := c.Size()
	panelW := min(float32(336), max(float32(240), w-40))
	backdropAlpha := float32(0.28)
	shadowAlpha := float32(0.16)
	if colors.Dark {
		backdropAlpha = 0.56
		shadowAlpha = 0.38
	}

	dialog := ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, backdropAlpha))
		panel.Label(opts.Title).
			Width(panelW).
			MaxHeight(max(float32(160), h-40)).
			Padding(20, 22).
			Radius(10).
			Background(colors.Panel).
			Border(1, colors.Border).
			Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, shadowAlpha))

		ui.Column(c).FillWidth().Gap(18).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, opts.Title).FontSize(16).FontWeight(600).TextColor(colors.Text)
				ui.Text(c, opts.Message).FillWidth().FontSize(13).LineHeight(1.55).TextColor(colors.Muted)
			})

			ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(8).Children(func() {
				style := dialogButtonStyle(colors)
				dialogButton(c.Key("cancel"), opts.CancelLabel, true, style, func() {
					*open = false
					if opts.OnCancel != nil {
						opts.OnCancel()
					}
				})
				dialogButton(c.Key("confirm"), opts.ConfirmLabel, false, style, func() {
					*open = false
					if opts.OnConfirm != nil {
						opts.OnConfirm()
					}
				})
			})
		})
	})

	if dialog.Dismissed() && opts.OnCancel != nil {
		opts.OnCancel()
	}
	return dialog
}

// GrayscaleSurface nudges fully opaque colors 1/255 below full alpha so MyGo
// uses grayscale text antialiasing instead of ClearType RGB subpixel fringes
// on 100%-DPI Windows CJK text when ancestor surfaces are also below 255.
func GrayscaleSurface(c ui.Color) ui.Color {
	if c.A == 255 {
		c.A = 254
	}
	return c
}

type buttonColors struct {
	bg, hover, pressed, border, text ui.Color
}

func dialogButtonStyle(colors ConfirmDialogColors) buttonColors {
	if colors.Dark {
		return buttonColors{
			bg:      GrayscaleSurface(ui.Hex("#2c2d30")),
			hover:   GrayscaleSurface(ui.Hex("#35373b")),
			pressed: GrayscaleSurface(ui.Hex("#3e4046")),
			border:  ui.Hex("#3a3c42"),
			text:    colors.Text,
		}
	}
	return buttonColors{
		bg:      GrayscaleSurface(ui.Hex("#f3f4f6")),
		hover:   GrayscaleSurface(ui.Hex("#ebedf0")),
		pressed: GrayscaleSurface(ui.Hex("#e2e5ea")),
		border:  ui.Hex("#e2e5ea"),
		text:    colors.Text,
	}
}

func dialogButton(c *ui.Context, label string, autoFocus bool, style buttonColors, onClick func()) {
	btn := ui.ButtonBase(c).
		Label(label).
		Radius(7).
		MinHeight(30).
		Padding(0, 14).
		Border(1, style.border).
		FocusRing(true).
		Cursor(ui.CursorPointer)
	if autoFocus {
		btn.AutoFocus()
	}
	bg := style.bg
	switch {
	case btn.Pressed():
		bg = style.pressed
	case btn.Hovered():
		bg = style.hover
	}
	btn.Background(bg).OnClick(onClick)
	btn.Children(func() {
		// 1 DIP bottom padding balances DirectWrite YaHei upper headroom on Windows.
		ui.Text(c, label).FontSize(13).TextColor(style.text).SingleLine().Padding(0, 0, 1, 0)
	})
}
