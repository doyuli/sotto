// Sotto is a translation tool that lives on hotkeys and a bubble: type
// to translate, translate a selection or a screenshot, and pin the
// bubble to keep it around.
package main

import (
	_ "embed"
	"log"

	"sotto/internal/app"
)

//go:embed resources/icon.png
var iconPNG []byte

func main() {
	if err := app.Run(iconPNG); err != nil {
		log.Fatal(err)
	}
}
