package snip

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestHiddenCaptureReclaimsAfterLeaseWithoutCanceling(t *testing.T) {
	// Hiding a submitted selection must reclaim its surface without canceling OCR.
	task := (&testGate{}).Begin()
	closes := 0
	screen := &snipScreen{used: true, captures: 1, captureTask: task, dispose: func() { closes++ }}
	s := &Manager{screens: map[int64]*snipScreen{1: screen}}
	s.reclaimHidden(true)
	if !screen.retired || closes != 0 || !task.Valid() {
		t.Fatal("hidden capture lost its lease or task")
	}
	screen.releaseCapture()
	if closes != 1 || !task.Valid() {
		t.Fatal("capture completion did not reclaim window")
	}
	s.reclaimHidden(false)
	if closes != 1 || len(s.screens) != 0 {
		t.Fatal("duplicate disposal or stale screen")
	}
}

func TestHiddenOverlayIdleCleanupRejectsStaleTimer(t *testing.T) {
	// A timer already posted to the UI must not destroy a later screenshot.
	synctest.Test(t, func(t *testing.T) {
		closes := 0
		screen := &snipScreen{dispose: func() { closes++ }}
		s := &Manager{screens: map[int64]*snipScreen{1: screen}}
		posted := make(chan func(), 1)
		s.scheduleIdleCleanup(func(fn func()) { posted <- fn })
		time.Sleep(overlayIdleTimeout)
		synctest.Wait()
		stale := <-posted
		s.stopIdleCleanup()
		screen.round = &snipRound{task: (&testGate{}).Begin()}
		stale()
		if closes != 0 {
			t.Fatal("stale timer destroyed active overlay")
		}
		screen.round = nil
		s.scheduleIdleCleanup(func(fn func()) { posted <- fn })
		time.Sleep(overlayIdleTimeout)
		synctest.Wait()
		(<-posted)()
		if closes != 1 || len(s.screens) != 0 {
			t.Fatal("idle overlay retained")
		}
	})
}
