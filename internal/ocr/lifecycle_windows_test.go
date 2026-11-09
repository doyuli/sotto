//go:build windows

package ocr

import (
	"context"
	"errors"
	"image"
	"strings"
	"testing"

	"sotto/internal/ocr/pipeline"
)

type lifecycleRunner struct {
	closes int
	err    error
}

func (*lifecycleRunner) Run(context.Context, pipeline.Tensor) (pipeline.Tensor, error) {
	return pipeline.Tensor{}, nil
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
