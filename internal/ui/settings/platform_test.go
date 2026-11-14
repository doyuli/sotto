package settings

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
)

func TestSettingsPlatformCopyAndShortcutPresentation(t *testing.T) {
	label, _ := credentialProtectionCopy(runtime.GOOS)
	themes, widths := []bool{false}, []int{780}
	if os.Getenv("SOTTO_SETTINGS_PREVIEW") != "" {
		themes, widths = []bool{false, true}, []int{660, 780}
	}
	for _, dark := range themes {
		for _, width := range widths {
			t.Run(fmt.Sprintf("dark=%t/width=%d", dark, width), func(t *testing.T) {
				v := NewView(TabHotkeys, testSettingsConfig(), Actions{})
				defer v.Dispose()
				tt := ui.NewTester(v.Render, width, 600)
				tt.SetDark(dark)
				tt.Frame()
				if !tt.HasText(label) {
					t.Fatalf("missing platform credential label: %v", tt.Texts())
				}
				for _, key := range []string{config.Default().Hotkeys.Panel, config.Default().Hotkeys.Ocr, config.Default().Hotkeys.Selection} {
					if !tt.HasText(displayHotkey(key)) {
						t.Fatalf("missing native shortcut %s", displayHotkey(key))
					}
				}
				if runtime.GOOS == "darwin" && tt.HasText("Windows 凭据保护") {
					t.Fatal("Windows credential label leaked into Mac settings")
				}
				if output := os.Getenv("SOTTO_SETTINGS_PREVIEW"); output != "" {
					if err := os.MkdirAll(output, 0755); err != nil {
						t.Fatal(err)
					}
					f, err := os.Create(filepath.Join(output, fmt.Sprintf("settings-%d-dark-%t.png", width, dark)))
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if err := png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
