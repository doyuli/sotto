//go:build windows

package ocr

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sotto/internal/ocr/pipeline"
)

func TestOCRResourcePaths(t *testing.T) {
	for _, checkout := range []bool{false, true} {
		name := "packaged"
		if checkout {
			name = "checkout"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("SOTTO_OCR_DIR", "")
			models := filepath.Join(root, "ocr")
			runtimeDir := models
			if checkout {
				models = filepath.Join(root, "resources", "windows", "ocr")
				runtimeDir = filepath.Join(root, "resources", "windows-"+runtime.GOARCH, "ocr")
			}
			for _, dir := range []string{models, runtimeDir} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			wantDLL := filepath.Join(runtimeDir, "onnxruntime.dll")
			if err := os.WriteFile(wantDLL, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if gotModels, gotDLL := ocrResourcePaths(); gotModels != models || gotDLL != wantDLL {
				t.Fatalf("resources = (%q, %q), want (%q, %q)", gotModels, gotDLL, models, wantDLL)
			}
		})
	}
	t.Run("override", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("SOTTO_OCR_DIR", dir)
		if models, dll := ocrResourcePaths(); models != dir || dll != filepath.Join(dir, "onnxruntime.dll") {
			t.Fatalf("override ignored: (%q, %q)", models, dll)
		}
	})
}

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
	modelDir := filepath.Join(root, "resources", "windows", "ocr")
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
			err = session.Run(context.Background(), pipeline.Tensor{Data: make([]float32, n), Shape: inputShape}, func(out pipeline.Tensor) error {
				output, shape := out.Data, out.Shape
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
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			checkORTConsumerExits(t, session, pipeline.Tensor{Data: make([]float32, n), Shape: inputShape})
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
					if err := session.Run(context.Background(), pipeline.Tensor{Data: make([]float32, preflightElements(t, inputs[i])), Shape: inputs[i]}, func(pipeline.Tensor) error { return nil }); err != nil {
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

// Every abnormal consumer exit must release the output and unlock the session
// before another request can use it. Real native calls remain opt-in.
func checkORTConsumerExits(t *testing.T, session *ortSession, input pipeline.Tensor) {
	t.Helper()
	failure := errors.New("consumer failure")
	if err := session.Run(context.Background(), input, func(pipeline.Tensor) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("lost consumer error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := session.Run(ctx, input, func(pipeline.Tensor) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost consumer cancellation: %v", err)
	}
	if err := session.Run(ctx, input, func(pipeline.Tensor) error {
		t.Fatal("canceled request consumed output")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request ran: %v", err)
	}
	func() {
		defer func() {
			if got := recover(); got != failure {
				t.Fatalf("lost consumer panic: %v", got)
			}
		}()
		_ = session.Run(context.Background(), input, func(pipeline.Tensor) error { panic(failure) })
	}()
	calls := 0
	if err := session.Run(context.Background(), input, func(pipeline.Tensor) error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("session did not recover after consumer exits: calls=%d err=%v", calls, err)
	}
}

// This bridge comparison reports Go allocations only. The copied case adds
// the former full-output transfer; both cases run the same native inference
// and checksum consumer. It excludes image preprocessing and CTC decoding.
func BenchmarkV6SmallRecognitionOutput(b *testing.B) {
	if os.Getenv("SOTTO_OCR_PREFLIGHT") != "1" {
		b.Skip("set SOTTO_OCR_PREFLIGHT=1 on Windows to benchmark the packaged runtime")
	}
	root := filepath.Join("..", "..")
	runtimeDir, err := filepath.Abs(filepath.Join(root, "resources", "windows-"+runtime.GOARCH, "ocr"))
	if err != nil {
		b.Fatal(err)
	}
	b.Setenv("SOTTO_OCR_DIR", runtimeDir)
	m, err := readManifest()
	if err != nil {
		b.Fatal(err)
	}
	spec := m.Models[1]
	session, err := newORTSession(filepath.Join(root, "resources", "windows", "ocr", spec.File), spec.InputName, spec.OutputName)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = session.Close() })
	input := pipeline.Tensor{Data: make([]float32, 3*48*320), Shape: []int64{1, 3, 48, 320}}
	if err := session.Run(context.Background(), input, func(pipeline.Tensor) error { return nil }); err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"borrowed", "copied"} {
		b.Run(mode, func(b *testing.B) {
			var checksum float64
			b.ReportAllocs()
			for b.Loop() {
				if err := session.Run(context.Background(), input, func(out pipeline.Tensor) error {
					data := out.Data
					if mode == "copied" {
						data = append([]float32(nil), data...)
					}
					for _, value := range data {
						checksum += float64(value)
					}
					return nil
				}); err != nil {
					b.Fatal(err)
				}
			}
			b.Logf("checksum=%g", checksum)
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
