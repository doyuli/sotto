//go:build darwin

package app

import (
	"bytes"
	"image/png"
	"os"
	"testing"

	"sotto/internal/platform"

	"github.com/egoist/mygo"
)

func TestMacAccessoryInitialization(t *testing.T) {
	platform.Init()
	if got := mygo.App.ActivationPolicy(); got != mygo.ActivationPolicyAccessory {
		t.Fatalf("activation policy = %q", got)
	}
	o := platform.SettingsWindow(mygo.WindowOptions{Title: "settings", Width: 780})
	if !o.Panel || !o.Hidden || o.Width != 780 {
		t.Fatalf("settings must be created as a hidden nonactivating panel: %+v", o)
	}
}

func TestMacTrayTemplateHasNoOpaqueBackground(t *testing.T) {
	iconPNG, err := os.ReadFile("../../resources/icon.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		icon []byte
	}{
		{"app icon", iconPNG},
		{"no icon", nil},
		{"invalid icon", []byte("not a PNG")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := platform.TrayOptions(mygo.TrayOptions{Icon: tt.icon, ToolTip: "Sotto 翻译"})
			if opts.ID != "main" || !opts.IconIsTemplate || opts.ToolTip != "Sotto 翻译" {
				t.Fatalf("unexpected tray options: %+v", opts)
			}
			img, err := png.Decode(bytes.NewReader(opts.Icon))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != 36 || img.Bounds().Dy() != 36 {
				t.Fatalf("expected 36x36 template icon, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
			}
			var ink, innerHole, antialias bool
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					r, g, b, a := img.At(x, y).RGBA()
					ink = ink || a == 0xffff
					antialias = antialias || (a > 0 && a < 0xffff)
					if r != 0 || g != 0 || b != 0 {
						t.Fatal("template must carry alpha only")
					}
					if (x == 0 || y == 0 || x == 35 || y == 35) && a != 0 {
						t.Fatal("template must have transparent padding on all sides")
					}
					// The S must remain transparent inside the opaque bubble.
					if y >= 10 && y <= 20 && x >= 15 && x <= 22 && a < 0x4000 {
						innerHole = true
					}
				}
			}
			if !ink || !antialias {
				t.Fatal("template must have opaque ink and antialiased edges")
			}
			if !innerHole {
				t.Fatal("expected S cutout inside the speech bubble")
			}
		})
	}
}

func TestWorkspaceCancellationPreservesNewEntry(t *testing.T) {
	var gate entryGate
	old := gate.begin()
	old.workspace++
	if old.Valid() {
		t.Fatal("old workspace entry stayed valid")
	}
	gate.cancelStaleWorkspace()
	if old.ctx.Err() == nil {
		t.Fatal("old workspace work was not canceled")
	}
	fresh := gate.begin()
	gate.cancelStaleWorkspace()
	if !fresh.Valid() {
		t.Fatal("delayed notification canceled a new workspace entry")
	}
	fresh.cancel()
}
