package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	bubbleScrollbarDelay = 900 * time.Millisecond
	bubbleScrollbarFade  = 150 * time.Millisecond
	bubbleScrollbarFrame = time.Second / 60
)

// Keep native scroll targets alive while hiding only their ink. Disabling
// Scrollbars would cancel a thumb drag when the pointer pauses before moving.
type bubbleScrollbar struct {
	x, y        float32
	initialized bool
	until       time.Time
	theme       ui.Theme
}

func (s *bubbleScrollbar) reset(scroll *ui.ScrollState) {
	s.x, s.y = scroll.X, scroll.Y
	s.initialized = true
	s.until = time.Time{}
}

func (s *bubbleScrollbar) update(scroll *ui.ScrollState, now time.Time) bool {
	if !s.initialized {
		s.reset(scroll)
	}
	if s.x != scroll.X || s.y != scroll.Y {
		s.x, s.y = scroll.X, scroll.Y
		s.until = now.Add(bubbleScrollbarDelay)
	}
	return now.Before(s.until)
}

// opacity leaves a full-strength thumb for the idle delay, then fades only
// its ink. A new offset restores it immediately, including during a held drag.
func (s *bubbleScrollbar) opacity(now time.Time) float32 {
	if s.until.IsZero() {
		return 0
	}
	if now.Before(s.until) {
		return 1
	}
	return max(0, 1-float32(now.Sub(s.until))/float32(bubbleScrollbarFade))
}

func (s *bubbleScrollbar) themed(c *ui.Context, scroll *ui.ScrollState, base *ui.Theme) *ui.Theme {
	now := c.Now()
	visible := s.update(scroll, now)
	opacity := s.opacity(now)
	s.theme = *base
	s.theme.Scrollbar = base.Scrollbar.Alpha(opacity)
	if visible {
		c.After(s.until.Sub(now))
	} else if opacity > 0 {
		// After uses the UI clock, so neither a goroutine nor a stored Context
		// is needed; no frames are requested once the fade has finished.
		c.After(min(bubbleScrollbarFrame, s.until.Add(bubbleScrollbarFade).Sub(now)))
	}
	return &s.theme
}
