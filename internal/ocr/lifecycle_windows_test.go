//go:build windows

package ocr

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	"sotto/internal/ocr/pipeline"

	"golang.org/x/sys/windows"
)

type lifecycleRunner struct {
	closes int
	err    error
}

func (*lifecycleRunner) Run(_ context.Context, _ pipeline.Tensor, consume func(pipeline.Tensor) error) error {
	return consume(pipeline.Tensor{})
}
func (r *lifecycleRunner) Close() error { r.closes++; return r.err }

func TestLocalIdleCloseErrorDiscardsCache(t *testing.T) {
	admission <- struct{}{}
	defer func() { <-admission }()
	previous := ppEngine
	defer func() { ppEngine = previous }()
	failure := errors.New("detector close failed")
	det, rec := &lifecycleRunner{err: failure}, &lifecycleRunner{}
	r, err := pipeline.New(det, rec, []byte(strings.Repeat("a\n", pipeline.DictionaryEntries)), pipeline.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ppEngine = r
	gone, err := releaseIdleResource(localOCR)
	if !gone || !errors.Is(err, failure) || ppEngine != nil || det.closes != 1 || rec.closes != 1 {
		t.Fatalf("failed close retained cache or skipped a runner: gone=%t err=%v", gone, err)
	}
	if gone, err := releaseIdleResource(localOCR); !gone || err != nil || det.closes != 1 || rec.closes != 1 {
		t.Fatal("empty cache retried a failed closer")
	}
}

func TestCanceledEngineEntryDoesNotRenewResource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, run := range []func(context.Context, image.Image, string, *idleRequest) (Text, error){recognizePPOCR, recognizeWinRT} {
		request := &idleRequest{}
		if _, err := run(ctx, nil, "", request); !errors.Is(err, context.Canceled) || request.used != (engineResources{}) {
			t.Fatalf("canceled entry renewed an unused engine: %v, %v", err, request.used)
		}
	}
}

func TestFailedRuntimeUnloadRetainsDLLForRetry(t *testing.T) {
	admission <- struct{}{}
	defer func() { <-admission }()
	previous := ortRT
	defer func() { ortRT = previous }()
	// Native objects have been released; only the loader reference needs retrying.
	const handle windows.Handle = 123
	ortRT = &ortRuntime{lib: handle}
	failure := errors.New("unload failed")
	if err := unloadORT(func(windows.Handle) error { return failure }); !errors.Is(err, failure) || !cachedOCRResources()[localOCR] {
		t.Fatalf("failed unload lost its retry: %v", err)
	}
	if err := unloadORT(func(h windows.Handle) error {
		if h != handle {
			t.Fatalf("retried wrong library: %v", h)
		}
		return nil
	}); err != nil || cachedOCRResources()[localOCR] {
		t.Fatalf("retry retained the cache: %v", err)
	}
}
