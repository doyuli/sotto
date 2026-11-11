package ocr

import (
	"context"
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"sotto/internal/ocr/pipeline"
)

type fakeIdleClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeIdleTimer
}

type fakeIdleTimer struct {
	clock  *fakeIdleClock
	at     time.Time
	f      func()
	active bool
}

func (t *fakeIdleTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := t.active
	t.active = false
	return was
}

func (c *fakeIdleClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeIdleClock) AfterFunc(d time.Duration, f func()) idleTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeIdleTimer{clock: c, at: c.now.Add(d), f: f, active: true}
	c.timers = append(c.timers, t)
	return t
}
func (c *fakeIdleClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	for _, t := range c.timers {
		if t.active && !c.now.Before(t.at) {
			t.active = false
			due = append(due, t.f)
		}
	}
	c.mu.Unlock()
	for _, f := range due {
		f()
	}
}
func (c *fakeIdleClock) active() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if t.active {
			n++
		}
	}
	return n
}
func (c *fakeIdleClock) latest() *fakeIdleTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.timers[len(c.timers)-1]
}

type gateCheckingIdleClock struct {
	*fakeIdleClock
	gate chan struct{}
	t    *testing.T
}

func (c gateCheckingIdleClock) AfterFunc(d time.Duration, f func()) idleTimer {
	if d == 0 && len(c.gate) != 0 {
		c.t.Error("immediate timer would race the completing request's admission token")
	}
	return c.fakeIdleClock.AfterFunc(d, f)
}

type idleFixture struct {
	coordinator *idleCoordinator
	clock       *fakeIdleClock
	resources   engineResources
	closed      [engineCount]int
}

func newIdleFixture() *idleFixture {
	f := &idleFixture{clock: &fakeIdleClock{now: time.Unix(100, 0)}}
	timeouts := idleTimeouts{localOCR: defaultLocalIdleTimeout, systemOCR: defaultSystemIdleTimeout}
	f.coordinator = newIdleCoordinator(make(chan struct{}, 1), f.clock, timeouts, func(id int) (bool, error) {
		f.closed[id]++
		f.resources[id] = false
		return true, nil
	})
	return f
}

func (f *idleFixture) warm(ids ...int) {
	r := f.coordinator.begin()
	f.coordinator.admission <- struct{}{}
	for _, id := range ids {
		f.resources[id], r.used[id] = true, true
	}
	r.done(&f.resources)
}

func TestIdleNoResourcesAndDeadline(t *testing.T) {
	f := newIdleFixture()
	if f.clock.active() != 0 || len(f.clock.timers) != 0 {
		t.Fatal("unused OCR created a timer")
	}
	f.warm(localOCR)
	f.clock.advance(defaultLocalIdleTimeout - time.Nanosecond)
	if f.closed[localOCR] != 0 {
		t.Fatal("released before the deadline")
	}
	callback := f.clock.latest().f
	f.clock.advance(time.Nanosecond)
	callback() // Even a duplicate delivery must not release twice.
	if f.closed[localOCR] != 1 || f.clock.active() != 0 {
		t.Fatalf("release count %v, timers %d", f.closed, f.clock.active())
	}
}

func TestIdleStaleCallbackCannotReplaceNewTimer(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR)
	old := f.clock.latest()
	f.clock.advance(4 * time.Minute)
	f.warm(localOCR)
	current := f.clock.latest()
	old.f() // Stop does not guarantee that a real callback is not running.
	if !current.active || f.clock.active() != 1 || f.closed[0] != 0 {
		t.Fatal("stale callback changed the current timer or resource")
	}
	f.clock.advance(defaultLocalIdleTimeout)
	if f.closed[0] != 1 {
		t.Fatal("new deadline was lost")
	}
}

func TestIdleEngineDeadlinesAreIndependent(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR, systemOCR)
	interval := defaultSystemIdleTimeout / 2
	f.clock.advance(interval)
	f.warm(localOCR)
	f.clock.advance(interval - time.Nanosecond)
	if f.closed != ([engineCount]int{}) {
		t.Fatalf("released before the system deadline: %v", f.closed)
	}
	f.clock.advance(time.Nanosecond)
	if f.closed != ([engineCount]int{0, 1}) || f.clock.active() != 1 {
		t.Fatalf("using local OCR changed the system deadline: %v", f.closed)
	}
	f.clock.advance(defaultLocalIdleTimeout - interval)
	if f.closed != ([engineCount]int{1, 1}) || f.clock.active() != 0 {
		t.Fatalf("local deadline was lost: %v", f.closed)
	}
}

func TestIdleExpiredEngineIsNotStarvedByOtherRequests(t *testing.T) {
	for _, expired := range []int{localOCR, systemOCR} {
		f := newIdleFixture()
		f.coordinator.clock = gateCheckingIdleClock{f.clock, f.coordinator.admission, t}
		active := 1 - expired
		f.warm(expired)
		lastUsed := f.coordinator.engines[expired].lastUsed
		f.clock.advance(f.coordinator.timeouts[expired] - 100*time.Millisecond)
		request := f.coordinator.begin()
		f.coordinator.admission <- struct{}{}
		request.used[active], f.resources[active] = true, true
		f.clock.advance(200 * time.Millisecond)
		request.done(&f.resources)
		if f.coordinator.engines[expired].lastUsed != lastUsed || f.clock.latest().at != f.clock.Now() {
			t.Fatal("another engine's completion postponed an overdue deadline")
		}
		// No quiet interval reaches the one-second contention backoff. The
		// overdue resource must still close while the other engine stays warm.
		for range 40 {
			f.clock.advance(500 * time.Millisecond)
			f.warm(active)
		}
		if f.closed[expired] != 1 || f.closed[active] != 0 || f.clock.active() != 1 {
			t.Fatalf("expired engine %d starved: releases=%v timers=%d", expired, f.closed, f.clock.active())
		}
	}
}

func TestIdleContentionRetryKeepsItsAbsoluteDeadline(t *testing.T) {
	for _, expired := range []int{localOCR, systemOCR} {
		for _, busyGate := range []bool{false, true} {
			f := newIdleFixture()
			active := 1 - expired
			f.warm(expired)
			if busyGate {
				f.coordinator.admission <- struct{}{}
			} else {
				release := f.coordinator.release
				f.coordinator.release = func(id int) (bool, error) {
					f.coordinator.release = release
					return false, nil
				}
			}
			f.clock.advance(f.coordinator.timeouts[expired])
			retry := f.clock.latest().at
			if busyGate {
				<-f.coordinator.admission
			}
			for range 2 {
				f.clock.advance(400 * time.Millisecond)
				f.warm(active)
				if f.clock.latest().at != retry || f.closed[expired] != 0 {
					t.Fatal("unrelated completion restarted or bypassed the contention backoff")
				}
			}
			f.clock.advance(200 * time.Millisecond)
			if f.closed[expired] != 1 || f.closed[active] != 0 || f.clock.active() != 1 {
				t.Fatalf("expired engine %d lost its contention retry: %v", expired, f.closed)
			}
		}
	}
}

func TestIdleBusyAdmissionAndWorkerAreRetried(t *testing.T) {
	for _, busyGate := range []bool{false, true} {
		f := newIdleFixture()
		f.warm(systemOCR)
		if busyGate {
			f.coordinator.admission <- struct{}{}
		} else {
			release := f.coordinator.release
			f.coordinator.release = func(id int) (bool, error) {
				f.coordinator.release = release
				return false, nil // The worker's TryLock failed.
			}
		}
		f.clock.advance(defaultSystemIdleTimeout)
		if f.closed[systemOCR] != 0 || f.clock.active() != 1 {
			t.Fatal("busy resource was released or lost its retry")
		}
		if busyGate {
			<-f.coordinator.admission
		}
		f.clock.advance(idleRetryDelay)
		if f.closed[systemOCR] != 1 || f.clock.active() != 0 {
			t.Fatal("resource was not released after becoming idle")
		}
	}
}

func TestIdleQueuedRequestsProtectResourcesAndRestoreTimer(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR)
	old := f.clock.latest()
	running := f.coordinator.begin()
	f.coordinator.admission <- struct{}{}
	queued := f.coordinator.begin()
	f.clock.advance(defaultLocalIdleTimeout)
	old.f()
	queued.done(nil)
	if f.closed[0] != 0 || f.clock.active() != 0 {
		t.Fatal("queued cancellation released an active inference")
	}
	running.used[localOCR] = true
	running.done(&f.resources)
	f.clock.advance(defaultLocalIdleTimeout - time.Nanosecond)
	if f.closed[0] != 0 {
		t.Fatal("idle time started before inference actually returned")
	}
	f.clock.advance(time.Nanosecond)
	if f.closed[0] != 1 {
		t.Fatal("completion did not restore scheduling")
	}
}

func TestIdleCommittedReleaseSurvivesNewCanceledRequest(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR)
	interval := defaultSystemIdleTimeout / 2
	f.clock.advance(defaultLocalIdleTimeout - interval)
	f.warm(systemOCR)
	started, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := f.coordinator.release
	f.coordinator.release = func(id int) (bool, error) {
		close(started)
		<-resume
		return release(id)
	}
	go func() { f.clock.advance(interval); close(finished) }()
	<-started
	queued := f.coordinator.begin()
	queued.done(nil)
	close(resume)
	<-finished
	f.coordinator.release = release
	if f.coordinator.releasing || f.coordinator.requests != 0 || len(f.coordinator.admission) != 0 || f.closed != ([engineCount]int{1, 0}) || f.clock.active() != 1 {
		t.Fatalf("committed release lost cleanup: %+v, %v", f.coordinator, f.closed)
	}
	f.clock.advance(interval)
	if f.closed[systemOCR] != 1 {
		t.Fatal("remaining resource lost its deadline")
	}
}

func TestIdleCloseErrorLeavesCacheTerminalAndCanReload(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR, systemOCR)
	release := f.coordinator.release
	f.coordinator.release = func(id int) (bool, error) {
		gone, _ := release(id)
		if id == localOCR {
			return gone, errors.New("close failed")
		}
		return gone, nil
	}
	f.clock.advance(defaultLocalIdleTimeout)
	f.clock.advance(time.Hour)
	if f.closed != ([engineCount]int{1, 1}) || f.clock.active() != 0 || f.coordinator.engines[0].loaded {
		t.Fatal("close error left a ghost cache or recurring retry")
	}
	f.coordinator.release = release
	for range 20 {
		f.warm(localOCR)
		f.warm(localOCR)
		if f.clock.active() != 1 {
			t.Fatal("repeated warm use created multiple timers")
		}
		f.clock.advance(defaultLocalIdleTimeout)
		if f.clock.active() != 0 || f.coordinator.releasing || len(f.coordinator.admission) != 0 {
			t.Fatal("release cycle leaked a timer or gate")
		}
	}
	if f.closed[0] != 21 {
		t.Fatalf("unexpected releases: %v", f.closed)
	}
}

func TestIdleInitializationFailureAndCanceledAttempt(t *testing.T) {
	f := newIdleFixture()
	r := f.coordinator.begin()
	f.coordinator.admission <- struct{}{}
	r.used[localOCR] = true
	r.done(&f.resources) // Initialization failed and produced no resource.
	if f.clock.active() != 0 || f.coordinator.requests != 0 {
		t.Fatal("failed initialization scheduled permanent retries")
	}
	f.warm(localOCR)
	f.clock.advance(time.Minute)
	r = f.coordinator.begin()
	f.coordinator.admission <- struct{}{}
	r.used[localOCR] = true
	f.clock.advance(defaultLocalIdleTimeout) // Canceled native work has not returned.
	if f.closed[0] != 0 {
		t.Fatal("cancellation released native work prematurely")
	}
	r.done(&f.resources)
	f.clock.advance(defaultLocalIdleTimeout)
	if f.closed[0] != 1 {
		t.Fatal("failed/canceled attempt did not renew its cached resource")
	}
}

func TestIdleTimeoutConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name         string
		local, winrt string
		want         idleTimeouts
	}{
		{"defaults", "", "", idleTimeouts{5 * time.Minute, 30 * time.Second}},
		{"local override only", "2s", "", idleTimeouts{2 * time.Second, 30 * time.Second}},
		{"Windows override only", "", "1m30s", idleTimeouts{5 * time.Minute, 90 * time.Second}},
		{"both overrides", "2s", "1m30s", idleTimeouts{2 * time.Second, 90 * time.Second}},
		{"invalid local override", "bad", "1m30s", idleTimeouts{5 * time.Minute, 90 * time.Second}},
		{"invalid Windows override", "2s", "bad", idleTimeouts{2 * time.Second, 30 * time.Second}},
		{"nonpositive overrides", "0", "-1s", idleTimeouts{5 * time.Minute, 30 * time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SOTTO_OCR_IDLE", tc.local)
			t.Setenv("SOTTO_OCR_WINRT_IDLE", tc.winrt)
			if got := configuredIdleTimeouts(); got != tc.want {
				t.Fatalf("idle timeouts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRequestEarlyReturnsRestoreExistingDeadlines(t *testing.T) {
	for _, mode := range []string{"queued cancel", "admitted cancel", "invalid image"} {
		t.Run(mode, func(t *testing.T) {
			f := newIdleFixture()
			f.warm(localOCR)
			last := f.coordinator.engines[0].lastUsed
			// The timer is due but its callback has not been dispatched yet.
			f.clock.mu.Lock()
			f.clock.now = f.clock.now.Add(defaultLocalIdleTimeout)
			f.clock.mu.Unlock()
			base, cancel := context.WithCancel(context.Background())
			var ctx context.Context = base
			defer cancel()
			if mode == "queued cancel" {
				cancel()
			} else if mode == "admitted cancel" {
				ctx = cancelOnErrorCheck{Context: base, cancel: cancel}
			}
			if mode == "queued cancel" {
				f.coordinator.admission <- struct{}{}
			}
			_, err := recognizeRequest(ctx, nil, "", f.coordinator,
				func(context.Context, image.Image, string, *idleRequest) (Text, error) {
					t.Fatal("early return reached an OCR engine")
					return Text{}, nil
				}, func() engineResources { return f.resources })
			if err == nil {
				t.Fatal("invalid request succeeded")
			}
			if mode == "queued cancel" {
				<-f.coordinator.admission
			}
			if f.coordinator.requests != 0 || f.coordinator.engines[0].lastUsed != last || f.clock.active() != 1 {
				t.Fatal("early return renewed the cache or lost its timer")
			}
			f.clock.advance(idleRetryDelay)
			if f.coordinator.engines[0].loaded || f.closed[0] != 1 || f.clock.active() != 0 {
				t.Fatal("expired resource survived early-return recovery")
			}
		})
	}
}

type cancelOnErrorCheck struct {
	context.Context
	cancel context.CancelFunc
}

func (c cancelOnErrorCheck) Err() error { c.cancel(); return c.Context.Err() }

func TestRequestFallbackSharesAdmissionAndRenewal(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR)
	old := f.clock.latest()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	_, err := recognizeRequest(context.Background(), img, "en", f.coordinator,
		func(ctx context.Context, img image.Image, lang string, request *idleRequest) (Text, error) {
			local := func(context.Context, image.Image, string) (Text, error) {
				request.used[localOCR] = true
				f.clock.advance(defaultLocalIdleTimeout)
				return Text{}, errors.New("local unavailable")
			}
			system := func(context.Context, image.Image, string) (Text, error) {
				old.f()
				select {
				case f.coordinator.admission <- struct{}{}:
					<-f.coordinator.admission
					t.Fatal("fallback released admission between attempts")
				default:
				}
				if f.closed[localOCR] != 0 {
					t.Fatal("local sessions closed during fallback")
				}
				request.used[systemOCR], f.resources[systemOCR] = true, true
				return Text{Lines: []pipeline.Line{testLine("fallback", 0, 0, 100, 20, -1)}}, nil
			}
			return recognizeEngines(ctx, img, lang, local, system)
		}, func() engineResources { return f.resources })
	if err != nil || f.clock.active() != 1 || f.coordinator.requests != 0 {
		t.Fatalf("fallback completion: %v, timers=%d", err, f.clock.active())
	}
	f.clock.advance(defaultSystemIdleTimeout)
	if f.closed != ([engineCount]int{0, 1}) {
		t.Fatalf("fallback must release the worker first: %v", f.closed)
	}
	f.clock.advance(defaultLocalIdleTimeout - defaultSystemIdleTimeout)
	if f.closed != ([engineCount]int{1, 1}) {
		t.Fatalf("both attempts must renew from completion: %v", f.closed)
	}
}

func TestCanceledNativeRequestKeepsAdmissionUntilActualReturn(t *testing.T) {
	f := newIdleFixture()
	f.warm(localOCR)
	old := f.clock.latest()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, resume := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := recognizeRequest(ctx, image.NewNRGBA(image.Rect(0, 0, 8, 8)), "", f.coordinator,
			func(_ context.Context, _ image.Image, _ string, request *idleRequest) (Text, error) {
				request.used[localOCR] = true
				close(started)
				<-resume // Simulate an ORT Run that cannot observe cancellation.
				return Text{Text: "late"}, nil
			}, func() engineResources { return f.resources })
		done <- err
	}()
	<-started
	cancel()
	f.clock.advance(defaultLocalIdleTimeout)
	old.f()
	select {
	case <-done:
		t.Fatal("cancellation claimed native work had already returned")
	default:
	}
	select {
	case f.coordinator.admission <- struct{}{}:
		<-f.coordinator.admission
		t.Fatal("canceled native request released admission early")
	default:
	}
	close(resume)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f.clock.advance(defaultLocalIdleTimeout - time.Nanosecond)
	if f.closed[localOCR] != 0 {
		t.Fatal("idle deadline started before native completion")
	}
	f.clock.advance(time.Nanosecond)
	if f.closed[localOCR] != 1 || len(f.coordinator.admission) != 0 {
		t.Fatal("native completion lost resource scheduling or admission")
	}
}
