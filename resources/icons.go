// Package resources embeds the application and tray artwork used at runtime.
package resources

import _ "embed"

//go:embed icon.png
var AppIcon []byte
