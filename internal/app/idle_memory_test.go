package app

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestIdleMemoryReclaimsOnceAfterLastActivity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		m := &idleMemory{reclaim: func() { calls++ }}
		advance := func(d time.Duration) {
			time.Sleep(d)
			synctest.Wait()
		}
		// A window reopening just before the deadline must protect even a hidden job.
		m.begin(true)()
		advance(14 * time.Minute)
		hide := m.begin(false)
		finish := m.begin(true)
		advance(20 * time.Minute)
		hide()
		advance(20 * time.Minute)
		finish()
		advance(15*time.Minute - time.Second)
		if calls != 0 {
			t.Fatal("reclaimed during activity or before a full idle interval")
		}
		advance(time.Second)
		m.begin(false)()
		advance(time.Hour)
		if calls != 1 {
			t.Fatalf("reclaimed %d times during one idle period", calls)
		}
		m.begin(true)()
		advance(15 * time.Minute)
		if calls != 2 {
			t.Fatal("new work did not rearm cleanup")
		}
	})
}
