//go:build !windows

package main

import "fmt"

// openInBrowser has no implementation off Windows, where the app does
// not build its UI anyway.
func openInBrowser(url string) error {
	return fmt.Errorf("opening a browser is only supported on Windows")
}
