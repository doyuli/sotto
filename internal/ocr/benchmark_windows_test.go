//go:build windows

package ocr

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sotto/internal/ocr/pipeline"
)

// Opt-in same-image comparison; the application keeps its normal defaults.
func BenchmarkDetectorThreads(b *testing.B) {
	path := os.Getenv("SOTTO_OCR_BENCH_IMAGE")
	if path == "" {
		b.Skip("set SOTTO_OCR_BENCH_IMAGE to an absolute screenshot PNG path")
	}
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		b.Fatal(err)
	}
	modelDir := filepath.Join("..", "..", "resources", "ocr")
	runtimeDir, err := filepath.Abs(filepath.Join("..", "..", "resources", "windows-"+runtime.GOARCH, "ocr"))
	if err != nil {
		b.Fatal(err)
	}
	b.Setenv("SOTTO_OCR_DIR", runtimeDir)
	m, err := readManifest()
	if err != nil {
		b.Fatal(err)
	}
	for _, a := range m.Assets {
		if err := verifyAsset(context.Background(), modelDir, a); err != nil {
			b.Fatal(err)
		}
	}
	dict, err := os.ReadFile(filepath.Join(modelDir, m.Dictionary.File))
	if err != nil {
		b.Fatal(err)
	}
	var reference string
	for _, threads := range []int{2, 4} {
		name := "2-threads"
		if threads == 4 {
			name = "4-threads"
		}
		b.Run(name, func(b *testing.B) {
			det, err := newORTSessionWithThreads(filepath.Join(modelDir, m.Models[0].File), m.Models[0].InputName, m.Models[0].OutputName, threads)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(det.close)
			rec, err := newORTSession(filepath.Join(modelDir, m.Models[1].File), m.Models[1].InputName, m.Models[1].OutputName)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(rec.close)
			r, err := pipeline.New(det, rec, dict, pipeline.DefaultConfig())
			if err != nil {
				b.Fatal(err)
			}
			warm, err := r.Recognize(context.Background(), img)
			if err != nil {
				b.Fatal(err)
			}
			text, err := finish(Text{Lines: warm.Lines})
			if err != nil {
				b.Fatal(err)
			}
			if threads == 2 {
				reference = text.Text
			} else if reference != text.Text {
				b.Fatal("thread count changed recognized text")
			}
			var detectionUS int64
			for b.Loop() {
				got, err := r.Recognize(context.Background(), img)
				if err != nil {
					b.Fatal(err)
				}
				detectionUS += got.Timings.DetectionInferenceUS
			}
			b.ReportMetric(float64(detectionUS)/float64(b.N), "det-us/op")
		})
	}
}
