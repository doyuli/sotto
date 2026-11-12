package pipeline

import (
	"context"
	"errors"
	"image"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

type callbackRunner func(context.Context, Tensor, func(Tensor) error) error

func (f callbackRunner) Run(ctx context.Context, in Tensor, consume func(Tensor) error) error {
	return f(ctx, in, consume)
}

func twoLineMap() Tensor {
	return probabilityMap(64, 64, func(x, y int) float32 {
		if x >= 10 && x <= 40 && ((y >= 8 && y <= 16) || (y >= 40 && y <= 48)) {
			return 1
		}
		return 0
	})
}

func TestBorrowedOutputsDoNotEscape(t *testing.T) {
	// Poison both Data and Shape as soon as each consumer returns, and reuse
	// recognition storage for every line and request as a native arena may do.
	det := callbackRunner(func(_ context.Context, _ Tensor, consume func(Tensor) error) error {
		out := twoLineMap()
		defer func() { clear(out.Data); clear(out.Shape) }()
		return consume(out)
	})
	ids := []int{1, 1, 0, 1, 2, Classes - 1, Classes - 1, 0}
	scores := []float32{.9, .2, 1, .7, .8, .6, .1, 1}
	storage := make([]float32, len(ids)*Classes)
	rec := callbackRunner(func(_ context.Context, _ Tensor, consume func(Tensor) error) error {
		clear(storage)
		for step, id := range ids {
			storage[step*Classes+id] = scores[step]
		}
		out := Tensor{Data: storage, Shape: []int64{1, int64(len(ids)), Classes}}
		defer func() {
			for i := range storage {
				storage[i] = -1
			}
			clear(out.Shape)
		}()
		return consume(out)
	})
	r, err := New(det, rec, testDictionary(), smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	first, err := r.Recognize(context.Background(), img)
	if err != nil || len(first.Lines) != 2 {
		t.Fatalf("borrowed detection or decoding failed: %+v %v", first, err)
	}
	for _, line := range first.Lines {
		var steps []int
		for _, token := range line.Tokens {
			steps = append(steps, token.Step)
		}
		if line.Text != "AA中 " || !line.HasScore || math.Abs(line.Score-.75) > 1e-6 || !reflect.DeepEqual(steps, []int{0, 3, 4, 5}) {
			t.Fatalf("borrowed output changed decoding semantics: %+v", line)
		}
	}
	second, err := r.Recognize(context.Background(), img)
	if err != nil || !reflect.DeepEqual(first.Lines, second.Lines) {
		t.Fatalf("a later run changed owned results: %+v %v", second, err)
	}
}

func TestInvalidBorrowedProbabilitiesNeverPublishPartialText(t *testing.T) {
	for _, invalid := range []float32{float32(math.NaN()), float32(math.Inf(1)), -.1, 1.1} {
		calls := 0
		det := runnerFunc(func(context.Context, Tensor) (Tensor, error) { return twoLineMap(), nil })
		rec := runnerFunc(func(context.Context, Tensor) (Tensor, error) {
			calls++
			out := ctcTensor(Classes, []int{1}, []float32{.9})
			if calls == 2 {
				// Validate every probability, including classes not selected by argmax.
				out.Data[Classes-1] = invalid
			}
			return out, nil
		})
		r, _ := New(det, rec, testDictionary(), smallConfig())
		result, err := r.Recognize(context.Background(), image.NewNRGBA(image.Rect(0, 0, 64, 64)))
		if err == nil || !strings.Contains(err.Error(), "invalid probability") || len(result.Lines) != 0 || calls != 2 {
			t.Fatalf("invalid probability %v published text: %+v %v calls=%d", invalid, result, err, calls)
		}
	}
}

func TestCancellationAfterBorrowedDecodeNeverPublishesLines(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	det := runnerFunc(func(context.Context, Tensor) (Tensor, error) { return twoLineMap(), nil })
	calls := 0
	rec := callbackRunner(func(_ context.Context, _ Tensor, consume func(Tensor) error) error {
		calls++
		err := consume(ctcTensor(Classes, []int{1}, []float32{1}))
		if calls == 2 {
			cancel()
		}
		return err
	})
	r, _ := New(det, rec, testDictionary(), smallConfig())
	result, err := r.Recognize(ctx, image.NewNRGBA(image.Rect(0, 0, 64, 64)))
	if !errors.Is(err, context.Canceled) || calls != 2 || len(result.Lines) != 0 {
		t.Fatalf("cancellation after decoding published text: %+v %v calls=%d", result, err, calls)
	}
}

func TestRunCancellationAndConsumerErrors(t *testing.T) {
	failure := errors.New("consumer failure")
	for _, phase := range []string{"before run", "before consume", "during consume", "after consume", "consumer error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if phase == "before run" {
				cancel()
			}
			runs, consumes := 0, 0
			released := false
			runner := callbackRunner(func(_ context.Context, _ Tensor, consume func(Tensor) error) error {
				runs++
				defer func() { released = true }()
				if phase == "before consume" {
					cancel()
				}
				err := consume(Tensor{})
				if phase == "after consume" {
					cancel()
				}
				return err
			})
			_, err := run(ctx, runner, Tensor{}, func(Tensor) error {
				consumes++
				if released {
					t.Fatal("output was released before consumption")
				}
				if phase == "during consume" {
					cancel()
				}
				return failure
			})
			want := error(context.Canceled)
			if phase == "consumer error" {
				want = failure
			}
			wantRuns, wantConsumes := 1, 1
			if phase == "before run" {
				wantRuns, wantConsumes = 0, 0
			} else if phase == "before consume" {
				wantConsumes = 0
			}
			if !errors.Is(err, want) || runs != wantRuns || consumes != wantConsumes || released != (wantRuns != 0) {
				t.Fatalf("err=%v runs=%d consumes=%d released=%t", err, runs, consumes, released)
			}
		})
	}
}

func TestRunTimingExcludesConsumer(t *testing.T) {
	var consumed time.Duration
	runner := callbackRunner(func(_ context.Context, _ Tensor, consume func(Tensor) error) error {
		return consume(Tensor{})
	})
	start := time.Now()
	inference, err := run(context.Background(), runner, Tensor{}, func(Tensor) error {
		start := time.Now()
		time.Sleep(10 * time.Millisecond)
		consumed = time.Since(start)
		return nil
	})
	if err != nil || inference < 0 || inference+consumed > time.Since(start) {
		t.Fatalf("consumer time was counted as inference: inference=%v consumed=%v err=%v", inference, consumed, err)
	}
}
