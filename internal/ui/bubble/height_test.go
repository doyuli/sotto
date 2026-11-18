package bubble

import (
	"testing"
	"time"
)

func TestBubbleHeightAnimationContinuesFromCurrentHeight(t *testing.T) {
	now := time.Unix(0, 0)
	var animation bubbleHeightAnimation
	animation.retarget(240, 264, now)
	early := animation.heightAt(now.Add(bubbleGrowDuration / 3))
	later := animation.heightAt(now.Add(2 * bubbleGrowDuration / 3))
	if early <= 240 || later <= early || later >= 264 || early-240 <= later-early {
		t.Fatalf("height did not grow with deceleration: 240, %d, %d, 264", early, later)
	}

	// A second toggle can happen before the first resize has finished.
	retargetedAt := now.Add(2 * bubbleGrowDuration / 3)
	animation.retarget(later, 280, retargetedAt)
	if got := animation.heightAt(retargetedAt); got != later {
		t.Fatalf("retarget started at %d instead of the current height %d", got, later)
	}
	if got := animation.heightAt(retargetedAt.Add(bubbleGrowDuration)); got != 280 {
		t.Fatalf("animation did not settle at the new target: %d", got)
	}
	collapsedAt := retargetedAt.Add(bubbleGrowDuration)
	animation.retarget(280, 200, collapsedAt)
	if got := animation.heightAt(collapsedAt.Add(bubbleShrinkDuration / 2)); got <= 200 || got >= 280 {
		t.Fatalf("shrink skipped the intermediate height: %d", got)
	}
}

func TestBubbleHeightAnimationMakesRoomForLargeResults(t *testing.T) {
	now := time.Unix(0, 0)
	var animation bubbleHeightAnimation
	// A long result used to spend the full transition partly outside the viewport.
	const measured = 580
	animation.retarget(240, measured, now)
	initial := animation.heightAt(now)
	if initial < measured-24 || initial >= measured {
		t.Fatalf("initial height = %d, want at most one line left to animate", initial)
	}
	for tick := bubbleResizeFrame; tick < bubbleGrowDuration; tick += bubbleResizeFrame {
		height := animation.heightAt(now.Add(tick))
		if height < initial || height > measured {
			t.Fatalf("height at %v = %d, want between %d and %d", tick, height, initial, measured)
		}
		initial = height
	}
	if got := animation.heightAt(now.Add(bubbleGrowDuration)); got != measured {
		t.Fatalf("settled height = %d, want measured height %d", got, measured)
	}
}
