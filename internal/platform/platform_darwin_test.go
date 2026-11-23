//go:build darwin

package platform

import (
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
