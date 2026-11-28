//go:build darwin

package platform

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

func ConfigureSelectionButtonWindow(w *mygo.Window, _ func()) error {
	panel := objc.ID(w.NativeHandle())
	panel.Send(macnative.Sel("setBecomesKeyOnlyIfNeeded:"), true)
	return nil
}
