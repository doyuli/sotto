//go:build darwin

package platform

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/egoist/mygo"
)

func TestMacAccessoryInitialization(t *testing.T) {
	Init()
	if got := mygo.App.ActivationPolicy(); got != mygo.ActivationPolicyAccessory {
		t.Fatalf("activation policy = %q", got)
	}
	o := SettingsWindow(mygo.WindowOptions{Title: "settings", Width: 780})
	if !o.Panel || !o.Hidden || o.Width != 780 {
		t.Fatalf("settings must be created as a hidden nonactivating panel: %+v", o)
	}
}

func TestMacTrayTemplateHasNoOpaqueBackground(t *testing.T) {
	opts := TrayOptions(mygo.TrayOptions{ToolTip: "Sotto 翻译"})
	if opts.ID != "main" || !opts.IconIsTemplate || opts.ToolTip != "Sotto 翻译" {
		t.Fatalf("options=%+v", opts)
	}
	img, err := png.Decode(bytes.NewReader(opts.Icon))
	if err != nil {
		t.Fatal(err)
	}
	var ink, transparent bool
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r != 0 || g != 0 || b != 0 {
				t.Fatal("template contains color")
			}
			ink = ink || a > 0
			transparent = transparent || a == 0
			if (x == img.Bounds().Min.X || y == img.Bounds().Min.Y || x == img.Bounds().Max.X-1 || y == img.Bounds().Max.Y-1) && a != 0 {
				t.Fatal("template has opaque padding")
			}
		}
	}
	if !ink || !transparent {
		t.Fatal("template must contain ink and transparency")
	}
}
