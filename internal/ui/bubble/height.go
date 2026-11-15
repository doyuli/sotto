package bubble

import (
	"math"
	"time"

	"github.com/egoist/mygo"
)

const (
	bubbleResizeDuration = 150 * time.Millisecond
	bubbleResizeFrame    = time.Second / 60
	// Two result lines leave room for incoming text without resizing on every wrap.
	bubbleStreamHeadroom = 48
)

type bubbleHeightAnimation struct {
	from, target int
	started      time.Time
	timer        *time.Timer
}

func (a *bubbleHeightAnimation) heightAt(now time.Time) int {
	progress := min(1, max(0, float64(now.Sub(a.started))/float64(bubbleResizeDuration)))
	remaining := 1 - progress
	return a.target + int(math.Round(float64(a.from-a.target)*remaining*remaining*remaining))
}

func bubbleStreamHeight(measured, currentTarget, limit, room int) int {
	// Only the actual content may force the frame past its preferred edge.
	limit = min(limit, max(measured, room))
	if measured <= currentTarget {
		return min(currentTarget, limit)
	}
	return min(measured+bubbleStreamHeadroom, limit)
}

func (b *bubble) stopHeightAnimation() {
	if animation := b.heightAnimation; animation != nil {
		animation.timer.Stop()
		b.heightAnimation = nil
	}
}

func (m *Manager) resizeToContent(b *bubble, measured float64, streaming bool) {
	m.mu.Lock()
	limit := b.maxHeight
	m.mu.Unlock()
	_, current := b.win.ContentSize()
	target := bubbleLayoutHeight(measured, limit)
	if streaming {
		currentTarget := current
		if animation := b.heightAnimation; animation != nil {
			currentTarget = animation.target
		}
		bounds := b.win.Bounds()
		placement := b.placement
		var work mygo.Rectangle
		if placement == nil {
			work = bubbleDisplay(bounds).WorkArea
		} else {
			work = placement.work
		}
		room := bubbleStreamRoom(bounds, work, placement) - (bounds.Height - current)
		target = bubbleStreamHeight(target, currentTarget, bubbleLayoutHeight(limit, limit), room)
	}
	if b.view.reduceMotion || !b.win.IsVisible() || current > max(bubbleMinHeight, int(limit)) {
		b.stopHeightAnimation()
		m.reflow(b, true, target)
		return
	}
	if animation := b.heightAnimation; animation != nil && animation.target == target {
		return
	}
	b.stopHeightAnimation()
	if target == current {
		return
	}
	animation := &bubbleHeightAnimation{from: current, target: target, started: time.Now()}
	b.heightAnimation = animation
	m.scheduleHeightFrame(b, animation)
}

func (m *Manager) scheduleHeightFrame(b *bubble, animation *bubbleHeightAnimation) {
	delay := min(bubbleResizeFrame, time.Until(animation.started.Add(bubbleResizeDuration)))
	animation.timer = time.AfterFunc(delay, func() {
		mygo.RunOnMain(func() {
			// A queued tick may outlive a retarget, dismissal, or reused window.
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
