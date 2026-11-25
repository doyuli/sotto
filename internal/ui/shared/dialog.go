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
			Radius(8).
			Background(colors.Panel).
			Border(1, colors.Border).
			Shadow(0, 10, 28, 0, ui.RGBA(0, 0, 0, shadowAlpha))

		ui.Column(c).FillWidth().Gap(18).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, opts.Title).FontSize(16).FontWeight(600).TextColor(colors.Text)
				ui.Text(c, opts.Message).FillWidth().FontSize(13).LineHeight(1.55).TextColor(colors.Muted)
			})

			ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Button(c, opts.CancelLabel).AutoFocus().Radius(4).Cursor(ui.CursorPointer).OnClick(func() {
					*open = false
					if opts.OnCancel != nil {
						opts.OnCancel()
					}
				})
				ui.Button(c, opts.ConfirmLabel).Radius(4).Cursor(ui.CursorPointer).OnClick(func() {
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
