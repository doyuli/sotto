//go:build windows

package ocr

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
			t.Cleanup(session.close)
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
}

func preflightElements(t *testing.T, shape []int64) int {
	t.Helper()
	n, err := tensorElements(shape)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
