//go:build darwin

package platform

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"testing"
	"testing/synctest"
	"time"
)

func TestMacUpdateRequiresExplicitInstallAndRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks, installs := 0, 0
	fail := true
	u := macUpdateController{ctx: ctx, check: func(context.Context) (*mygo.Update, error) { checks++; return &mygo.Update{Version: "9.0.0"}, nil }, install: func(context.Context, *mygo.Update) error {
		installs++
		if fail {
			return errors.New("download failed")
		}
		return nil
	}}
	u.run(false)
	u.run(false)
	if checks != 1 || installs != 0 || u.update == nil {
		t.Fatal("background installed or repeated discovery")
	}
	u.run(true)
	if !u.failed || u.installed {
		t.Fatal("failed install was accepted")
	}
	fail = false
	u.run(true)
	u.run(true)
	if installs != 2 || !u.installed {
		t.Fatal("retry or installed guard failed")
	}
}

func TestMacUpdateRejectsConcurrentRequestsAndCancelsOnQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, done := make(chan struct{}), make(chan struct{})
	u := macUpdateController{ctx: ctx, check: func(ctx context.Context) (*mygo.Update, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }}
	go func() { u.run(false); close(done) }()
	<-entered
	u.run(true)
	cancel()
	<-done
	if u.busy || !u.failed {
		t.Fatal("quit did not settle the in-flight check")
	}
	u.run(true)
}

func TestMacAutomaticChecksStopScheduling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		u := macUpdateController{ctx: ctx, check: func(context.Context) (*mygo.Update, error) { calls++; return nil, nil }}
		u.setAutomatic(true)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if calls != 1 {
			t.Fatalf("checks=%d", calls)
		}
		u.setAutomatic(false)
		time.Sleep(25 * time.Hour)
		synctest.Wait()
		if calls != 1 {
			t.Fatal("disabled schedule continued")
		}
	})
}
