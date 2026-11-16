//go:build !windows && !darwin

package capture

import (
	"context"
	"errors"
)

func captureDisplays(context.Context, []int64, func(snapshotResult)) error {
	return errors.New("capture is only supported on Windows and macOS")
}
