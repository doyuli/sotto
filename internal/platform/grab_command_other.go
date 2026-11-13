//go:build !darwin

package platform

import (
	"context"

	"sotto/internal/selection"
)

func runGrabCommand(ctx context.Context, path string) (selection.Selection, error) {
	return grabCommandPath(ctx, path)
}
