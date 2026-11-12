package pipeline

import (
	"context"
	"errors"
	"fmt"
	"math"
)

const DictionaryEntries = 18708
const Classes = DictionaryEntries + 2

var ErrLimit = errors.New("ocr pipeline: resource limit exceeded")

type Tensor struct {
	Data  []float32
	Shape []int64
}

type Runner interface {
	// Run borrows input until it returns. After successful inference it calls
	// consume synchronously once. Output storage is read-only and valid only
	// inside consume; it must not be retained or used to reenter the runner.
	// Run propagates consumer errors unless canceled and releases output on
	// every exit, including cancellation and panic.
	Run(ctx context.Context, input Tensor, consume func(Tensor) error) error
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Quad [4]Point

type Token struct {
	Text  string  `json:"text"`
	Score float64 `json:"score"`
	Step  int     `json:"step"`
}

type Line struct {
	Text           string  `json:"text"`
	Quad           Quad    `json:"quad"`
	Score          float64 `json:"score"`
	HasScore       bool    `json:"hasScore"`
	DetectionScore float64 `json:"detectionScore"`
	Tokens         []Token `json:"tokens"`
}

type Diagnostics struct {
	Components       int `json:"components"`
	SmallRegions     int `json:"smallRegions"`
	LowScoreRegions  int `json:"lowScoreRegions"`
	DegenerateBoxes  int `json:"degenerateBoxes"`
	EmptyLines       int `json:"emptyLines"`
	DetectorWidth    int `json:"detectorWidth"`
	DetectorHeight   int `json:"detectorHeight"`
	RecognitionCalls int `json:"recognitionCalls"`
}

// Timings uses microseconds, accumulated across all recognition lines. Runner
// time includes the native bridge and cleanup, but excludes output consumers.
type Timings struct {
	DetectionPreprocessUS   int64 `json:"detectionPreprocessUs"`
	DetectionInferenceUS    int64 `json:"detectionInferenceUs"`
	DetectionPostprocessUS  int64 `json:"detectionPostprocessUs"`
	CropUS                  int64 `json:"cropUs"`
	RecognitionPreprocessUS int64 `json:"recognitionPreprocessUs"`
	RecognitionInferenceUS  int64 `json:"recognitionInferenceUs"`
	DecodeUS                int64 `json:"decodeUs"`
}

type Result struct {
	Lines       []Line
	Diagnostics Diagnostics
	Timings     Timings
}

// Config is explicit: start with DefaultConfig and change individual fields.
// Detection resizes the full image to its budget. Other limits reject the
// request, never truncate text or silently squeeze long recognition crops.
type Config struct {
	DetectorSide      int  // Minimum short-side target before resource caps.
	DetectorLongSide  int  // Normal long-side cap, independent of the hard limit.
	DetectorRoundUp   bool // Bounded comparison profile preserves small glyphs at alignment.
	Threshold         float64
	BoxThreshold      float64
	UnclipRatio       float64
	Dilation          bool
	MaxImagePixels    int
	MaxImageSide      int
	MaxDetectorPixels int
	MaxDetectorSide   int
	MaxCandidates     int
	MaxBoundaryPoints int
	MaxScorePixels    int
	MaxCropPixels     int
	MaxRecWidth       int
	MaxTensorElements int
}

func DefaultConfig() Config {
	return Config{
		DetectorSide: 448, DetectorLongSide: 4096, Threshold: .3, BoxThreshold: .5, UnclipRatio: 1.6, Dilation: true,
		MaxImagePixels: 16 << 20, MaxImageSide: 16384,
		MaxDetectorPixels: 1 << 20, MaxDetectorSide: 4096,
		MaxCandidates: 1000, MaxBoundaryPoints: 256 << 10, MaxScorePixels: 16 << 20,
		MaxCropPixels: 4 << 20, MaxRecWidth: 2048, MaxTensorElements: 32 << 20,
	}
}

func (c Config) validate() error {
	for _, v := range []int{c.DetectorSide, c.DetectorLongSide, c.MaxImagePixels, c.MaxImageSide,
		c.MaxDetectorPixels, c.MaxDetectorSide, c.MaxCandidates, c.MaxBoundaryPoints,
		c.MaxScorePixels, c.MaxCropPixels, c.MaxRecWidth, c.MaxTensorElements} {
		if v <= 0 || v > 1<<28 {
			return fmt.Errorf("ocr pipeline: limits must be in [1, 2^28]")
		}
	}
	if c.DetectorSide < 32 || c.DetectorLongSide < 32 || c.DetectorLongSide%32 != 0 || c.MaxDetectorSide < c.DetectorLongSide || c.MaxDetectorSide < c.DetectorSide || c.MaxRecWidth < 320 ||
		!probability(c.Threshold) || !probability(c.BoxThreshold) ||
		!finite(c.UnclipRatio) || c.UnclipRatio <= 0 || c.UnclipRatio > 10 {
		return fmt.Errorf("ocr pipeline: invalid detector/recognizer configuration")
	}
	return nil
}

func finite(v float64) bool      { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func probability(v float64) bool { return finite(v) && v >= 0 && v <= 1 }

func elements(shape []int64, limit int) (int, error) {
	n := int64(1)
	if len(shape) == 0 {
		return 0, fmt.Errorf("empty tensor shape")
	}
	for _, d := range shape {
		if d <= 0 {
			return 0, fmt.Errorf("invalid tensor shape %v", shape)
		}
		if d > int64(limit)/n {
			return 0, fmt.Errorf("%w: tensor %v", ErrLimit, shape)
		}
		n *= d
	}
	return int(n), nil
}

func validateTensor(ctx context.Context, t Tensor, rank, limit int) error {
	if len(t.Shape) != rank {
		return fmt.Errorf("expected rank %d, got %v", rank, t.Shape)
	}
	n, err := elements(t.Shape, limit)
	if err != nil {
		return err
	}
	if n != len(t.Data) {
		return fmt.Errorf("tensor shape %v needs %d elements, got %d", t.Shape, n, len(t.Data))
	}
	for i, v := range t.Data {
		if i%16384 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		// Both fixed graphs expose probabilities, not logits.
		if !finite(float64(v)) || v < -1e-6 || v > 1+1e-6 {
			return fmt.Errorf("invalid probability at %d", i)
		}
	}
	return ctx.Err()
}
