// Package capture takes display snapshots before the selection windows show.
package capture

import (
	"context"
	"errors"
	"fmt"
	"image"
	"time"
)

// MaxSnapshotPixels bounds the combined uncompressed images for one selection.
const MaxSnapshotPixels = 64 << 20

type DisplaySnapshot struct {
	ID    int64
	Image image.Image
}

type snapshotResult struct {
	frame DisplaySnapshot
	err   error
}

// Only one native worker may own a capture at a time. A caller can time out
// while a native callback or GDI call still owns resources in that worker.
var nativeCaptureAdmission = make(chan struct{}, 1)

// SnapshotDisplays takes one batch in priority order. A display failing or
// exceeding the remaining budget leaves the other captured displays usable.
func SnapshotDisplays(ctx context.Context, ids []int64) ([]DisplaySnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return collectSnapshots(ctx, ids, captureDisplays)
}

func collectSnapshots(ctx context.Context, ids []int64, run func(context.Context, []int64, func(snapshotResult)) error) ([]DisplaySnapshot, error) {
	results := make(chan snapshotResult)
	go func() {
		defer close(results)
		select {
		case nativeCaptureAdmission <- struct{}{}:
			defer func() { <-nativeCaptureAdmission }()
		case <-ctx.Done():
			return
		}
		if ctx.Err() != nil {
			return
		}
		emit := func(result snapshotResult) {
			select {
			case results <- result:
			case <-ctx.Done():
			}
		}
		if err := run(ctx, ids, emit); err != nil {
			emit(snapshotResult{err: err})
		}
	}()

	frames := make([]DisplaySnapshot, 0, len(ids))
	var failures []error
	for {
		select {
		case <-ctx.Done():
			return frames, errors.Join(append(failures, ctx.Err())...)
		case result, open := <-results:
			if !open {
				return frames, errors.Join(append(failures, ctx.Err())...)
			}
			if result.err != nil {
				failures = append(failures, result.err)
			} else {
				frames = append(frames, result.frame)
			}
		}
	}
}

func captureEachDisplay(ctx context.Context, ids []int64, grab func(int64, int) (image.Image, error), emit func(snapshotResult)) {
	remaining := MaxSnapshotPixels
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		img, err := grab(id, remaining)
		if err != nil {
			emit(snapshotResult{err: fmt.Errorf("snapshot display %d: %w", id, err)})
			continue
		}
		bounds := img.Bounds()
		remaining -= bounds.Dx() * bounds.Dy()
		emit(snapshotResult{frame: DisplaySnapshot{ID: id, Image: img}})
	}
}
