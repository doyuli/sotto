package ocr

import (
	"fmt"
	"strconv"
	"strings"

	"sotto/internal/ocr/pipeline"
)

// Profiles are read once when local sessions initialize. Keep the measured
// short-side policy until screenshot quality tests justify bounded resizing.
func detectorConfig(profile string) (pipeline.Config, error) {
	c := pipeline.DefaultConfig()
	switch strings.TrimSpace(profile) {
	case "", "balanced":
	case "bounded":
		c.DetectorSide, c.DetectorLongSide, c.DetectorRoundUp = 64, 960, true
	case "legacy":
		c.DetectorSide, c.MaxDetectorPixels = 736, 4<<20
	default:
		return c, fmt.Errorf("ocr: SOTTO_OCR_PROFILE must be balanced, bounded or legacy")
	}
	return c, nil
}

func detectorThreads(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 2, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("ocr: SOTTO_OCR_DETECTOR_THREADS: %w", err)
	}
	if n < 1 || n > 16 {
		return 0, fmt.Errorf("ocr: SOTTO_OCR_DETECTOR_THREADS must be in [1, 16]")
	}
	return n, nil
}
