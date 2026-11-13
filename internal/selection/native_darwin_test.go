//go:build darwin

package selection

import "testing"

func TestMacSelectionSymbols(t *testing.T) {
	if err := loadAX(); err != nil {
		t.Fatal(err)
	}
	if err := loadCopy(); err != nil {
		t.Fatal(err)
	}
}
