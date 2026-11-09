//go:build e2e && windows

package e2e

import "testing"

// TestMain has already set process awareness. A repeated request must accept
// that context rather than mistake ERROR_ACCESS_DENIED for virtualized input.
func TestDPIAwarenessAlreadyInitialized(t *testing.T) {
	if err := setTestDPIAwareness(); err != nil {
		t.Fatal(err)
	}
}
