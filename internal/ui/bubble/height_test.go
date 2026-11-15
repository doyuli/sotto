package bubble

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestBubbleHeightAnimationContinuesFromCurrentHeight(t *testing.T) {
	now := time.Unix(0, 0)
	animation := bubbleHeightAnimation{from: 240, target: 500, started: now}
	early := animation.heightAt(now.Add(bubbleResizeDuration / 3))
	later := animation.heightAt(now.Add(2 * bubbleResizeDuration / 3))
	if early <= 240 || later <= early || later >= 500 || early-240 <= later-early {
		t.Fatalf("height did not grow with deceleration: 240, %d, %d, 500", early, later)
	}

	// New text can change the target before an earlier frame is done.
	retargetedAt := now.Add(2 * bubbleResizeDuration / 3)
	animation = bubbleHeightAnimation{from: later, target: 350, started: retargetedAt}
	if got := animation.heightAt(retargetedAt); got != later {
		t.Fatalf("retarget started at %d instead of the current height %d", got, later)
	}
	if got := animation.heightAt(retargetedAt.Add(bubbleResizeDuration / 2)); got <= 350 || got >= later {
		t.Fatalf("shrink skipped the intermediate height: %d", got)
	}
	if got := animation.heightAt(retargetedAt.Add(2 * bubbleResizeDuration)); got != 350 {
		t.Fatalf("animation did not settle at the new target: %d", got)
	}
}

func TestBubbleStreamHeightCoalescesNewLines(t *testing.T) {
	target := 240
	changes := 0
	for _, measured := range []int{260, 284, 308, 332, 356, 380} {
		next := bubbleStreamHeight(measured, target, 370, 370)
		if next != target {
			changes++
		}
		if next < min(measured, 370) || next > 370 {
			t.Fatalf("target %d for measured height %d clips content or exceeds the display cap", next, measured)
		}
		target = next
	}
	// One frame per new line made the window move throughout the stream.
	if changes != 2 || target != 370 {
		t.Fatalf("six measurements produced %d resizes and final height %d", changes, target)
	}
}

func TestBubbleStreamHeadroomPreservesAnchor(t *testing.T) {
	work := mygo.Rectangle{Width: 1200, Height: 900}
	above := newBubblePlacement(mygo.Display{WorkArea: work}, mygo.Point{X: 500, Y: 366}, false, 480)
	above.above = true
	for _, tt := range []struct {
		name      string
		bounds    mygo.Rectangle
		placement *bubblePlacement
	}{
		{"pinned or dragged", mygo.Rectangle{X: 500, Y: 550, Width: 480, Height: 300}, nil},
		{"upward anchor", mygo.Rectangle{Width: 480, Height: 300}, &above},
	} {
		t.Run(tt.name, func(t *testing.T) {
			initial := fitBubbleFrame(tt.bounds, work, tt.placement)
			// Gratuitous headroom used to push the frame away even though
			// the measured text fit at its preferred edge.
			room := bubbleStreamRoom(initial, work, tt.placement)
			grown := initial
			grown.Height = bubbleStreamHeight(330, initial.Height, bubbleHeightLimit(work), room)
			grown = fitBubbleFrame(grown, work, tt.placement)
			settled := grown
			settled.Height = 330
			settled = fitBubbleFrame(settled, work, tt.placement)
			exact := initial
			exact.Height = 330
			exact = fitBubbleFrame(exact, work, tt.placement)
			if grown.Height != 338 || settled != exact {
				t.Fatalf("initial=%+v grown=%+v settled=%+v want=%+v", initial, grown, settled, exact)
			}
			if got := bubbleStreamHeight(350, grown.Height, bubbleHeightLimit(work), room); got != 350 {
				t.Fatalf("the anchor limited measured content to %d", got)
			}
		})
	}
}
