package ocr

import (
	"fmt"

	"sotto/internal/ocr/pipeline"
)

// Check products before converting native dimensions to Go slice lengths.
func tensorElements(shape []int64) (int, error) {
	const limit = 32 << 20
	if len(shape) == 0 || len(shape) > 4 {
		return 0, fmt.Errorf("ocr: invalid tensor rank")
	}
	n := int64(1)
	for _, d := range shape {
		if d <= 0 {
			return 0, fmt.Errorf("ocr: invalid tensor dimension")
		}
		if d > limit/n {
			return 0, fmt.Errorf("%w: native tensor", pipeline.ErrLimit)
		}
		n *= d
	}
	return int(n), nil
}
