//go:build windows

package screencoords

import "testing"

func TestMissingMonitorDPIAPIUsesSystemFallback(t *testing.T) {
	original := getDPIForMonitor
	getDPIForMonitor = user32.NewProc("SottoMissingGetDpiForMonitor")
	t.Cleanup(func() { getDPIForMonitor = original })
	if got := monitorDPI(1, 144); got != 144 {
		t.Errorf("missing monitor DPI API = %d, want system DPI 144", got)
	}
}

func TestMissingSystemDPIAPIUses96(t *testing.T) {
	original := getDPIForSystem
	getDPIForSystem = user32.NewProc("SottoMissingGetDpiForSystem")
	t.Cleanup(func() { getDPIForSystem = original })
	if got := systemDPI(); got != 96 {
		t.Errorf("missing system DPI API = %d, want 96", got)
	}
}
