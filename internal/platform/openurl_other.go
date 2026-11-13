//go:build !windows && !darwin

package platform

import "fmt"

// OpenInBrowser is unavailable on platforms without a supported backend.
func OpenInBrowser(url string) error {
	return fmt.Errorf("opening a browser is only supported on Windows and macOS")
}
