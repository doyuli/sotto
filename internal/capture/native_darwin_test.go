//go:build darwin

package capture

import "testing"

func TestMacCaptureSymbols(t *testing.T) {
	if err := loadCapture(); err != nil {
		t.Fatal(err)
	}
}
