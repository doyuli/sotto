package ocr

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"
)

func TestCanceledOCRDoesNotWaitBehindInference(t *testing.T) {
	admission <- struct{}{}
	defer func() { <-admission }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := Recognize(ctx, image.NewNRGBA(image.Rect(0, 0, 8, 8))); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request remained queued")
	}
}
