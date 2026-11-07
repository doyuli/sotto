package translate

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	chatFirstByteTimeout = 60 * time.Second
	chatIdleTimeout      = 30 * time.Second
)

var errChatIdleTimeout = errors.New("chat response idle timeout")

// chatActivity cancels only its request, never a shared HTTP connection. Timer
// callbacks recheck the last activity because Reset may race a fired callback.
type chatActivity struct {
	mu      sync.Mutex
	cancel  context.CancelCauseFunc
	timer   *time.Timer
	last    time.Time
	wait    time.Duration
	stopped bool
}

func newChatActivity(ctx context.Context) (context.Context, *chatActivity) {
	ctx, cancel := context.WithCancelCause(ctx)
	a := &chatActivity{cancel: cancel, last: time.Now(), wait: chatFirstByteTimeout}
	a.timer = time.AfterFunc(a.wait, a.expire)
	return ctx, a
}

func (a *chatActivity) expire() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	if remaining := a.wait - time.Since(a.last); remaining > 0 {
		a.timer.Reset(remaining)
		return
	}
	a.stopped = true
	a.cancel(errChatIdleTimeout)
}

func (a *chatActivity) reset(wait time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	a.last, a.wait = time.Now(), wait
	a.timer.Reset(wait)
}

func (a *chatActivity) stop() {
	a.mu.Lock()
	a.stopped = true
	a.timer.Stop()
	a.mu.Unlock()
	a.cancel(nil)
}

// Reset on bytes, including reasoning and heartbeat events, rather than on
// visible translation tokens. A thinking model is still making progress.
type chatActivityReader struct {
	io.Reader
	activity *chatActivity
}

func (r chatActivityReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.activity.reset(chatIdleTimeout)
	}
	return n, err
}
