package bubble

import (
	"math"
	"time"

	"github.com/egoist/mygo"
)

const (
	bubbleGrowDuration   = 100 * time.Millisecond
	bubbleShrinkDuration = 150 * time.Millisecond
	bubbleResizeFrame    = time.Second / 60
	bubbleGrowTail       = 24
)

type bubbleHeightAnimation struct {
	from, target int
	started      time.Time
	duration     time.Duration
	timer        *time.Timer
}

func (a *bubbleHeightAnimation) retarget(current, target int, now time.Time) {
	a.from, a.target = current, target
	a.started = now
	a.duration = bubbleShrinkDuration
	if target > current {
		// Make room for a large batch immediately so the resize only
		// trails the content by at most about one line of translation.
		a.from = max(current, target-bubbleGrowTail)
		a.duration = bubbleGrowDuration
	}
}

func (a *bubbleHeightAnimation) heightAt(now time.Time) int {
	progress := min(1, max(0, float64(now.Sub(a.started))/float64(a.duration)))
	remaining := 1 - progress
	return a.target + int(math.Round(float64(a.from-a.target)*remaining*remaining*remaining))
}

func (b *bubble) stopHeightAnimation() {
	if animation := b.heightAnimation; animation != nil {
		animation.timer.Stop()
		b.heightAnimation = nil
	}
}

func (m *Manager) resizeToContent(b *bubble, measurement bubbleHeightMeasurement) {
	m.mu.Lock()
	limit := b.maxHeight
	m.mu.Unlock()
	_, current := b.win.ContentSize()
	target := bubbleLayoutHeight(measurement.height, limit)
	if measurement.firstResult || !measurement.animate || b.view.reduceMotion || !b.win.IsVisible() || current > max(bubbleMinHeight, int(limit)) {
		b.stopHeightAnimation()
		m.reflow(b, true, target)
		// Place the first result beside the anchor before fixing the top edge.
		b.keepTop = b.keepTop || measurement.firstResult
		return
	}
	animation := b.heightAnimation
	if animation != nil && animation.target == target {
		return
	}
	if target == current {
		b.stopHeightAnimation()
		return
	}
	start := animation == nil
	if start {
		animation = &bubbleHeightAnimation{}
		b.heightAnimation = animation
	}
	animation.retarget(current, target, time.Now())
	// Keep the queued tick when the user toggles another card mid-transition.
	if start {
		m.scheduleHeightFrame(b, animation)
	}
	if animation.from != current {
		m.reflow(b, true, animation.from)
	}
}

func (m *Manager) scheduleHeightFrame(b *bubble, animation *bubbleHeightAnimation) {
	delay := min(bubbleResizeFrame, time.Until(animation.started.Add(animation.duration)))
	animation.timer = time.AfterFunc(delay, func() {
		mygo.RunOnMain(func() {
			// A queued tick may outlive a dismissal or a reused window.
			if b.heightAnimation != animation {
				return
			}
			height := animation.heightAt(time.Now())
			if b.view.reduceMotion {
				height = animation.target
			}
			if height == animation.target {
				b.heightAnimation = nil
			}
			m.reflow(b, true, height)
			if b.heightAnimation == animation {
				m.scheduleHeightFrame(b, animation)
			}
		})
	})
}
