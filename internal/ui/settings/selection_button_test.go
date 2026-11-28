package settings

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sotto/internal/config"
)

func TestSelectionButtonSettingSavesBothValues(t *testing.T) {
	saved := make(chan bool, 2)
	v := NewView(TabGeneral, config.Default(), Actions{SaveSettings: func(next config.Settings) error {
		saved <- next.SelectionButton
		return nil
	}})
	defer v.Dispose()
	tester := ui.NewTester(v.Render, 780, 600)
	tester.Frame()
	for _, want := range []bool{true, false} {
		if err := tester.Click("切换划词翻译按钮"); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-saved:
			if got != want {
				t.Fatalf("saved selection button = %t, want %t", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("selection button preference was not saved")
		}
	}
}
