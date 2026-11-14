package ocr

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

func TestNativeCancellationLifetime(t *testing.T) {
	// Cancellation must reach a running native operation without releasing its
	// run options while the interrupt callback still uses them.
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		started, interrupted, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
		returned := make(chan error, 1)
		go func() {
			returned <- withNativeCancellation(ctx, func() { close(interrupted); <-finish }, func() error {
				close(started)
				<-interrupted
				return errors.New("native termination")
			})
		}()
		<-started
		cancel()
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("released native ownership before interrupt completed")
		default:
		}
		close(finish)
		if err := <-returned; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestNativeCancellationNormalAndPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		failure := errors.New("native failure")
		if err := withNativeCancellation(ctx, func() { t.Error("late interrupt") }, func() error { return failure }); err != failure {
			t.Fatal(err)
		}
		func() {
			defer func() {
				if recover() != failure {
					t.Error("lost native panic")
				}
			}()
			_ = withNativeCancellation(ctx, func() { t.Error("late panic interrupt") }, func() error { panic(failure) })
		}()
		cancel()
		synctest.Wait()
		if err := withNativeCancellation(ctx, func() { t.Error("pre-canceled interrupt") }, func() error {
			t.Error("pre-canceled operation ran")
			return nil
		}); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}
