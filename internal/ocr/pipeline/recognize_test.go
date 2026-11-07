package pipeline

import (
	"context"
	"errors"
	"image"
	"reflect"
	"testing"
	"time"
)

type runnerFunc func(context.Context, Tensor) (Tensor, error)

func (f runnerFunc) Run(ctx context.Context, in Tensor) (Tensor, error) { return f(ctx, in) }

func TestRecognizeWithInjectedRunners(t *testing.T) {
	detCalls, recCalls := 0, 0
	det := runnerFunc(func(_ context.Context, in Tensor) (Tensor, error) {
		detCalls++
		time.Sleep(time.Millisecond)
		if !reflect.DeepEqual(in.Shape, []int64{1, 3, 64, 64}) {
			t.Fatal(in.Shape)
		}
		return probabilityMap(64, 64, func(x, y int) float32 {
			if x >= 8 && x <= 40 && y >= 20 && y <= 30 {
				return .9
			}
			return 0
		}), nil
	})
	rec := runnerFunc(func(_ context.Context, in Tensor) (Tensor, error) {
		recCalls++
		time.Sleep(time.Millisecond)
		if !reflect.DeepEqual(in.Shape, []int64{1, 3, 48, 320}) {
			t.Fatal(in.Shape)
		}
		return ctcTensor(Classes, []int{1, 0, 2}, []float32{.4, 1, .9}), nil
	})
	r, err := New(det, rec, testDictionary(), smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Recognize(context.Background(), image.NewNRGBA(image.Rect(100, 200, 164, 264)))
	if err != nil {
		t.Fatal(err)
	}
	if detCalls != 1 || recCalls != 1 || len(result.Lines) != 1 || result.Lines[0].Text != "A中" {
		t.Fatalf("calls %d %d result %+v", detCalls, recCalls, result)
	}
	if result.Diagnostics.RecognitionCalls != 1 || result.Timings.DetectionInferenceUS < 1000 || result.Timings.RecognitionInferenceUS < 1000 {
		t.Fatalf("missing inference timings: %+v %+v", result.Diagnostics, result.Timings)
	}
	line := result.Lines[0]
	if !line.HasScore || len(line.Tokens) != 2 || line.Score >= .8 || line.Quad[0].X >= 64 {
		t.Fatalf("lost score/coordinates %+v", line)
	}
	if len(Assess(result).Risks) == 0 {
		t.Fatal("low confidence not retained")
	}
	doc, err := Layout(result.Lines, LayoutOptions{})
	if err != nil || doc.Text != "A中" {
		t.Fatalf("layout %+v %v", doc, err)
	}
}

func TestCancellationAndFailureNeverPublishPartialText(t *testing.T) {
	called := 0
	ctx, cancel := context.WithCancel(context.Background())
	det := runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) { called++; cancel(); return Tensor{}, nil })
	rec := runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) {
		t.Fatal("recognizer called after cancellation")
		return Tensor{}, nil
	})
	r, _ := New(det, rec, testDictionary(), smallConfig())
	result, err := r.Recognize(ctx, image.NewNRGBA(image.Rect(0, 0, 32, 32)))
	if !errors.Is(err, context.Canceled) || len(result.Lines) != 0 || called != 1 {
		t.Fatalf("late result %+v %v calls %d", result, err, called)
	}
	_, err = r.Recognize(ctx, nil)
	if !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal("canceled request ran inference")
	}
	failure := errors.New("native failure")
	det = runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) { return Tensor{}, failure })
	r, _ = New(det, rec, testDictionary(), smallConfig())
	if _, err := r.Recognize(context.Background(), image.NewNRGBA(image.Rect(0, 0, 32, 32))); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	// Two detections, but a failure on the second recognizer call must not
	// publish the first line as a successful, silently truncated result.
	det = runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) {
		return probabilityMap(64, 64, func(x, y int) float32 {
			if x >= 10 && x <= 40 && ((y >= 8 && y <= 16) || (y >= 40 && y <= 48)) {
				return 1
			}
			return 0
		}), nil
	})
	called = 0
	rec = runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) {
		called++
		if called == 2 {
			return Tensor{}, failure
		}
		return ctcTensor(Classes, []int{1}, []float32{1}), nil
	})
	r, _ = New(det, rec, testDictionary(), smallConfig())
	result, err = r.Recognize(context.Background(), image.NewNRGBA(image.Rect(0, 0, 64, 64)))
	if !errors.Is(err, failure) || len(result.Lines) != 0 || called != 2 {
		t.Fatalf("partial publish %+v %v %d", result, err, called)
	}
}

func TestEmptyDetectionAndConfig(t *testing.T) {
	det := runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) {
		return probabilityMap(32, 32, func(int, int) float32 { return 0 }), nil
	})
	rec := runnerFunc(func(_ context.Context, _ Tensor) (Tensor, error) {
		t.Fatal("empty map called rec")
		return Tensor{}, nil
	})
	r, err := New(det, rec, testDictionary(), smallConfig())
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Recognize(context.Background(), image.NewNRGBA(image.Rect(0, 0, 32, 32)))
	if err != nil || len(result.Lines) != 0 || len(Assess(result).Risks) == 0 {
		t.Fatalf("empty result %+v %v", result, err)
	}
	if _, err := New(det, rec, testDictionary(), Config{}); err == nil {
		t.Fatal("zero config accepted")
	}
	if _, err := New(nil, rec, testDictionary(), smallConfig()); err == nil {
		t.Fatal("nil runner accepted")
	}
}
