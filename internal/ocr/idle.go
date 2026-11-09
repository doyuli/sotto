package ocr

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

const (
	defaultIdleTimeout = 5 * time.Minute
	idleRetryDelay     = time.Second
	localOCR           = 0
	systemOCR          = 1
	engineCount        = 2
)

type engineResources [engineCount]bool

type idleTimer interface{ Stop() bool }
type idleClock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) idleTimer
}

type wallIdleClock struct{}

func (wallIdleClock) Now() time.Time { return time.Now() }
func (wallIdleClock) AfterFunc(d time.Duration, f func()) idleTimer {
	return time.AfterFunc(d, f)
}

type idleEngine struct {
	loaded   bool
	lastUsed time.Time
}

// admission is the only resource lock. mu protects scheduling metadata and is
// never held while waiting for admission, a worker, or a native call.
type idleCoordinator struct {
	mu         sync.Mutex
	admission  chan struct{}
	clock      idleClock
	timeout    time.Duration
	release    func(int) (bool, error)
	engines    [engineCount]idleEngine
	requests   int
	releasing  bool
	generation uint64
	timer      idleTimer
}

func newIdleCoordinator(gate chan struct{}, clock idleClock, timeout time.Duration, release func(int) (bool, error)) *idleCoordinator {
	return &idleCoordinator{admission: gate, clock: clock, timeout: timeout, release: release}
}

type idleRequest struct {
	owner *idleCoordinator
	used  engineResources
}

func (c *idleCoordinator) begin() *idleRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests++
	c.invalidateLocked()
	return &idleRequest{owner: c}
}

// A nil snapshot means the request canceled before taking admission; existing
// resources and their deadlines must survive that early return unchanged.
func (r *idleRequest) done(snapshot *engineResources) {
	c := r.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests--
	if snapshot != nil {
		now := c.clock.Now()
		for i, loaded := range snapshot {
			c.engines[i].loaded = loaded
			if !loaded {
				c.engines[i].lastUsed = time.Time{}
			} else if r.used[i] {
				c.engines[i].lastUsed = now
			}
		}
	}
	c.scheduleLocked()
}

func (c *idleCoordinator) invalidateLocked() {
	c.generation++
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

func (c *idleCoordinator) scheduleLocked() {
	c.invalidateLocked()
	if c.requests != 0 || c.releasing {
		return
	}
	var next time.Time
	for _, e := range c.engines {
		if deadline := e.lastUsed.Add(c.timeout); e.loaded && (next.IsZero() || deadline.Before(next)) {
			next = deadline
		}
	}
	if next.IsZero() {
		return
	}
	delay := next.Sub(c.clock.Now())
	if delay <= 0 {
		delay = idleRetryDelay
	}
	generation := c.generation
	c.timer = c.clock.AfterFunc(delay, func() { c.expire(generation) })
}

func (c *idleCoordinator) expire(generation uint64) {
	c.mu.Lock()
	if c.generation != generation {
		c.mu.Unlock()
		return
	}
	// Consuming the timer also rejects a duplicate invocation of this callback.
	c.invalidateLocked()
	claim := c.generation
	c.mu.Unlock()
	select {
	case c.admission <- struct{}{}:
		defer func() { <-c.admission }()
	default:
		c.mu.Lock()
		if c.generation == claim {
			c.scheduleLocked()
		}
		c.mu.Unlock()
		return
	}
	c.mu.Lock()
	if c.generation != claim || c.requests != 0 || c.releasing {
		c.mu.Unlock()
		return
	}
	var due engineResources
	for i, e := range c.engines {
		due[i] = e.loaded && !c.clock.Now().Before(e.lastUsed.Add(c.timeout))
	}
	if due == (engineResources{}) {
		c.scheduleLocked()
		c.mu.Unlock()
		return
	}
	c.releasing = true
	c.mu.Unlock()

	var released engineResources
	for i, expired := range due {
		if expired {
			var err error
			released[i], err = c.release(i)
			if err != nil {
				log.Printf("ocr: release idle %s: %v", [engineCount]string{"ppocr-v6-small", "winrt"}[i], err)
			}
		}
	}

	c.mu.Lock()
	// Once committed, release must finish even if a new request changed the
	// generation. Replan from current metadata rather than an old snapshot.
	for i, gone := range released {
		if gone {
			c.engines[i] = idleEngine{}
		}
	}
	c.releasing = false
	c.scheduleLocked()
	c.mu.Unlock()
}

func idleTimeout(value string) (time.Duration, error) {
	if value == "" {
		return defaultIdleTimeout, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return defaultIdleTimeout, fmt.Errorf("ocr: SOTTO_OCR_IDLE: %w", err)
	}
	if d <= 0 {
		return defaultIdleTimeout, fmt.Errorf("ocr: SOTTO_OCR_IDLE must be positive")
	}
	return d, nil
}

func configuredIdleTimeout() time.Duration {
	d, err := idleTimeout(os.Getenv("SOTTO_OCR_IDLE"))
	if err != nil {
		log.Printf("%v; using %s", err, d)
	}
	return d
}
