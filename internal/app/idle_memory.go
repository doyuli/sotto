package app

import (
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// Visible windows and running jobs share one idle deadline. A quiet startup
// or an already reclaimed idle period needs no further collections.
type idleMemory struct {
	mu      sync.Mutex
	active  int
	dirty   bool
	timer   *time.Timer
	reclaim func()
}

func (m *idleMemory) begin(job bool) func() {
	if m == nil {
		return func() {}
	}
	m.mu.Lock()
	m.active++
	m.dirty = m.dirty || job
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	return m.end
}

func (m *idleMemory) end() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active--
	if m.active != 0 || !m.dirty {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(15*time.Minute, func() {
		m.mu.Lock()
		if m.timer != timer {
			m.mu.Unlock()
			return
		}
		m.timer = nil
		m.dirty = false
		m.mu.Unlock()
		m.reclaim()
	})
	m.timer = timer
}

func (m *idleMemory) watchWindow(w *mygo.Window) {
	var hidden func()
	show := func() {
		if hidden == nil {
			hidden = m.begin(false)
		}
	}
	hide := func() {
		if hidden != nil {
			hidden()
			hidden = nil
		}
	}
	w.OnShow(show)
	w.OnHide(hide)
	w.OnClosed(hide)
	if w.IsVisible() {
		show()
	}
}
