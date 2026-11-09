//go:build windows

package ocr

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sotto/internal/ocr/pipeline"
)

// This opt-in probe checks native model loading and tensor contracts.
// It does not measure real screenshot accuracy. Enable with SOTTO_OCR_PREFLIGHT=1.
func TestV6SmallRuntimePreflight(t *testing.T) {
	if os.Getenv("SOTTO_OCR_PREFLIGHT") != "1" {
		t.Skip("set SOTTO_OCR_PREFLIGHT=1 on Windows to test the packaged v6 assets")
	}
	if ortRT != nil {
		t.Fatal("preflight requires a fresh process before the shared runtime initializes")
	}
	root := filepath.Join("..", "..")
	modelDir := filepath.Join(root, "resources", "ocr")
	m, err := readManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range m.Assets {
		if err := verifyAsset(context.Background(), modelDir, asset); err != nil {
			t.Fatal(err)
		}
	}

	// A failed load must not poison the process-wide cache. The model probes
	// below must recover with the real DLLs without resetting runtime state.
	t.Run("missing runtime", func(t *testing.T) {
		t.Setenv("SOTTO_OCR_DIR", t.TempDir())
		if rt, err := sharedORT(); err == nil || rt != nil {
			t.Fatalf("missing runtime: %v %v", rt, err)
		}
	})

	// Read the architecture's existing DLLs without copying them into a temporary
	// folder: Windows keeps a loaded DLL locked until the process exits.
	runtimeDir, err := filepath.Abs(filepath.Join(root, "resources", "windows-"+runtime.GOARCH, "ocr"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOTTO_OCR_DIR", runtimeDir)
	inputs := [][]int64{{1, 3, 64, 64}, {1, 3, 48, 320}}
	for i, spec := range m.Models {
		inputShape := inputs[i]
		t.Run(spec.File, func(t *testing.T) {
			session, err := newORTSession(filepath.Join(modelDir, spec.File), spec.InputName, spec.OutputName)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			n := preflightElements(t, inputShape)
			output, shape, err := session.run(make([]float32, n), inputShape)
			if err != nil {
				t.Fatal(err)
			}
			if len(shape) != spec.OutputRank || shape[0] != 1 {
				t.Fatalf("unexpected output shape %v", shape)
			}
			if spec.OutputClasses != 0 && shape[len(shape)-1] != spec.OutputClasses {
				t.Fatalf("output classes %v, want %d", shape, spec.OutputClasses)
			}
			if spec.OutputRank == 4 && shape[1] != 1 {
				t.Fatalf("detector must return one probability map, got %v", shape)
			}
			if len(output) != preflightElements(t, shape) {
				t.Fatalf("output length %d does not match shape %v", len(output), shape)
			}
			for i, value := range output {
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < -1e-6 || value > 1+1e-6 {
					t.Fatalf("invalid probability at %d: %v", i, value)
				}
			}
			t.Logf("ORT API %d, windows/%s: %v -> %v; zero-tensor smoke only", ortAPIVersion, runtime.GOARCH, inputShape, shape)
		})
	}
	t.Run("idle release and rebuild retains runtime", func(t *testing.T) {
		rt := ortRT
		env, mem, lib := rt.env, rt.memInfo, rt.lib
		dictionary, err := os.ReadFile(filepath.Join(modelDir, m.Dictionary.File))
		if err != nil {
			t.Fatal(err)
		}
		admission <- struct{}{}
		defer func() { <-admission }()
		for cycle := range 20 {
			det, err := newORTSession(filepath.Join(modelDir, m.Models[0].File), m.Models[0].InputName, m.Models[0].OutputName)
			if err != nil {
				t.Fatal(err)
			}
			rec, err := newORTSession(filepath.Join(modelDir, m.Models[1].File), m.Models[1].InputName, m.Models[1].OutputName)
			if err != nil {
				_ = det.Close()
				t.Fatal(err)
			}
			r, err := pipeline.New(det, rec, dictionary, pipeline.DefaultConfig())
			if err != nil {
				_ = det.Close()
				_ = rec.Close()
				t.Fatal(err)
			}
			ppEngine = r
			if cycle == 19 {
				// Run again after repeated release, not just CreateSession, so
				// the retained shared memory-info object is actually exercised.
				for i, session := range []*ortSession{det, rec} {
					if _, _, err := session.run(make([]float32, preflightElements(t, inputs[i])), inputs[i]); err != nil {
						_, _ = releaseIdleResource(localOCR)
						t.Fatal(err)
					}
				}
			}
			gone, err := releaseIdleResource(localOCR)
			if !gone || err != nil || ppEngine != nil || det.ptr != 0 || rec.ptr != 0 {
				t.Fatalf("cycle %d did not release both cached sessions: %v", cycle, err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if cached, err := sharedORT(); err != nil || cached != rt || det.rt != rt || rec.rt != rt || rt.env != env || rt.memInfo != mem || rt.lib != lib {
				t.Fatalf("cycle %d changed shared runtime ownership: %v", cycle, err)
			}
		}
	})
}

func preflightElements(t *testing.T, shape []int64) int {
	t.Helper()
	n, err := tensorElements(shape)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
