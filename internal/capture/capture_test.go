package capture

import (
	"context"
	"errors"
	"image"
	"testing"
	"testing/synctest"
	"time"
)

// sizedSnapshot supplies geometry without allocating the full desktop.
type sizedSnapshot struct {
	image.Image
	bounds image.Rectangle
}

func (img sizedSnapshot) Bounds() image.Rectangle { return img.bounds }

func TestSnapshotBatchKeepsDisplaysAfterFailure(t *testing.T) {
	// A display beyond the snapshot budget used to remove all other previews.
	main := sizedSnapshot{bounds: image.Rect(0, 0, 6016, 3384)}
	secondary := sizedSnapshot{bounds: image.Rect(0, 0, 1920, 1080)}
	grab := func(id int64, remaining int) (image.Image, error) {
		switch id {
		case 1:
			if remaining != MaxSnapshotPixels {
				t.Errorf("initial budget = %d", remaining)
			}
			return main, nil
		case 2:
			return nil, ErrLimit
		case 3:
			if want := MaxSnapshotPixels - main.bounds.Dx()*main.bounds.Dy(); remaining != want {
				t.Errorf("remaining budget = %d, want %d", remaining, want)
			}
			return secondary, nil
		default:
			t.Errorf("unexpected display ID %d", id)
			return nil, errors.New("unexpected display")
		}
	}
	frames, err := collectSnapshots(t.Context(), []int64{1, 2, 3}, func(ctx context.Context, ids []int64, emit func(snapshotResult)) error {
		captureEachDisplay(ctx, ids, grab, emit)
		return nil
	})
	if !errors.Is(err, ErrLimit) || len(frames) != 2 || frames[0].ID != 1 || frames[1].ID != 3 {
		t.Fatalf("captured displays = %v, err = %v", frames, err)
	}
}

func TestSnapshotDeadlineReturnsReadyDisplays(t *testing.T) {
	// Native cleanup can still be pending after the deadline. Keep ready screens
	// and let the native worker finish independently from the caller.
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		finish := make(chan struct{})
		defer close(finish)
		frames, err := collectSnapshots(ctx, []int64{1, 2}, func(_ context.Context, _ []int64, emit func(snapshotResult)) error {
			emit(snapshotResult{frame: DisplaySnapshot{ID: 1, Image: image.NewRGBA(image.Rect(0, 0, 4, 4))}})
			<-finish
			return nil
		})
		if !errors.Is(err, context.DeadlineExceeded) || len(frames) != 1 || frames[0].ID != 1 {
			t.Fatalf("captured displays = %v, err = %v", frames, err)
		}
	})
}
