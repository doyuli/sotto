//go:build darwin

package platform

import "github.com/egoist/mygo"

func OpenInBrowser(url string) error { return mygo.Shell.OpenExternal(url) }
