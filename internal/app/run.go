// Package app assembles Sotto's lifecycle, hotkeys, services and entry tasks.
package app

import (
	"github.com/egoist/mygo"

	"sotto/internal/platform"
	"sotto/resources"
)

// Run handles diagnostic commands or runs the app until it quits. Call it
// from the main goroutine.
func Run() error {
	platform.Init()
	if platform.HandleNativeCommand() {
		return nil
	}
	if platform.HandleGrabCommand() {
		return nil
	}
	if platform.HandleOCRCommand() {
		return nil
	}
	// The first instance keeps the lock; a second one hands its command
	// line over and quits.
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	platform.InitUpdates(resources.AppIcon)
	a := newApplication()
	mygo.App.WhenReady(a.ready)
	return mygo.App.Run()
}
