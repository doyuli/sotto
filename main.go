// Sotto is a translation tool that lives on hotkeys and a bubble: type
// to translate, translate a selection or a screenshot, and pin the
// bubble to keep it around.
package main

import (
	"log"

	"sotto/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
