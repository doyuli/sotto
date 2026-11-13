package snip

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

func TestCanceledCaptureReleasesWindowLease(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var gate testGate
			task := gate.Begin()
			if revoke {
				gate.Begin()
			} else {
				task.Cancel()
			}
			released := 0
			shot := &Capture{Task: task, release: func() { released++ }}
			image, err := shot.Grab()
			if image != nil || !errors.Is(err, context.Canceled) || released != 1 {
				t.Fatalf("canceled capture: image=%v err=%v releases=%d", image, err, released)
			}
		})
	}
}
