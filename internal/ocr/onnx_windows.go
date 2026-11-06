//go:build windows

package ocr

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A thin, cgo-free wrapper over the ONNX Runtime C API. The project is
// built with CGO_ENABLED=0, so the official Go bindings (which use cgo)
// cannot be used; this file loads onnxruntime.dll and walks the OrtApi
// function table directly. The slot numbers come from onnxruntime's
// onnxruntime_c_api.h for API version 31 (ORT 1.31), which is the
// version shipped next to the PP-OCR models.

const (
	ortAPIVersion = 31

	ortSlotGetErrorMessage                = 2
	ortSlotCreateEnv                      = 3
	ortSlotCreateSession                  = 7
	ortSlotRun                            = 9
	ortSlotCreateSessionOptions           = 10
	ortSlotSetGraphOptimizationLevel      = 23
	ortSlotSetIntraOpNumThreads           = 24
	ortSlotCreateTensorWithDataAsOrtValue = 49
	ortSlotGetTensorMutableData           = 51
	ortSlotGetDimensionsCount             = 61
	ortSlotGetDimensions                  = 62
	ortSlotGetTensorShapeElementCount     = 64
	ortSlotGetTensorTypeAndShape          = 65
	ortSlotCreateCpuMemoryInfo            = 69
	ortSlotReleaseMemoryInfo              = 94
	ortSlotReleaseStatus                  = 93
	ortSlotReleaseSession                 = 95
	ortSlotReleaseValue                   = 96
	ortSlotReleaseTensorTypeAndShape      = 99
	ortSlotReleaseSessionOptions          = 100

	ortLoggingError     = 3
	ortOptimizeAll      = 99
	ortTensorFloat      = 1
	ortDeviceAllocator  = 0
	ortMemTypeDefault   = 0
	ortLoadLibraryFlags = 0x00000100 | 0x00001000 // DLL load dir + default dirs
)

type ortRuntime struct {
	lib     windows.Handle
	api     uintptr
	env     uintptr
	memInfo uintptr
}

var (
	ortOnce sync.Once
	ortRT   *ortRuntime
	ortErr  error
)

// sharedORT returns the process-wide runtime, initializing it on first
// use. A missing onnxruntime.dll or model directory surfaces as err so
// the engine chain can fall back to the Windows recognizer.
func sharedORT() (*ortRuntime, error) {
	ortOnce.Do(func() {
		dir := ocrResourceDir()
		dll := filepath.Join(dir, "onnxruntime.dll")
		if _, err := os.Stat(dll); err != nil {
			ortErr = fmt.Errorf("ocr: %s not found", dll)
			return
		}
		h, err := windows.LoadLibraryEx(dll, 0, ortLoadLibraryFlags)
		if err != nil {
			// Older loaders reject the flag combination; the plain call
			// still resolves onnxruntime_providers_shared.dll when it sits
			// beside the executable or in the resource directory on PATH.
			h, err = windows.LoadLibrary(dll)
			if err != nil {
				ortErr = fmt.Errorf("ocr: load onnxruntime: %w", err)
				return
			}
		}
		proc, err := windows.GetProcAddress(h, "OrtGetApiBase")
		if err != nil {
			ortErr = fmt.Errorf("ocr: OrtGetApiBase: %w", err)
			return
		}
		base, _, _ := syscall.SyscallN(proc)
		if base == 0 {
			ortErr = fmt.Errorf("ocr: OrtGetApiBase returned nil")
			return
		}
		// OrtApiBase is a struct of two function pointers; GetApi is first.
		getAPI := *(*uintptr)(native(base))
		api, _, _ := syscall.SyscallN(getAPI, ortAPIVersion)
		if api == 0 {
			ortErr = fmt.Errorf("ocr: onnxruntime API %d unavailable", ortAPIVersion)
			return
		}
		rt := &ortRuntime{lib: h, api: api}

		logid, _ := syscall.BytePtrFromString("sotto")
		var env uintptr
		if err := rt.check(rt.call(ortSlotCreateEnv, ortLoggingError,
			uintptr(unsafe.Pointer(logid)), uintptr(unsafe.Pointer(&env))), "CreateEnv"); err != nil {
			ortErr = err
			return
		}
		rt.env = env

		var mem uintptr
		if err := rt.check(rt.call(ortSlotCreateCpuMemoryInfo,
			ortDeviceAllocator, ortMemTypeDefault, uintptr(unsafe.Pointer(&mem))), "CreateCpuMemoryInfo"); err != nil {
			ortErr = err
			return
		}
		rt.memInfo = mem
		ortRT = rt
	})
	return ortRT, ortErr
}

// ocrResourceDir is where the model, dictionary and DLL live: beside the
// executable in a built app, or the repository's resources directory when
// running from a test or `go run`.
func ocrResourceDir() string {
	if dir := os.Getenv("SOTTO_OCR_DIR"); dir != "" {
		return dir
	}
	// `mygo build` copies the contents of resources/ next to the
	// executable (resources/ocr/* becomes <exe>/ocr/*), so look there
	// first, then at the repository layout for tests and `go run`.
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "ocr"),
			filepath.Join(base, "resources", "ocr"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "resources", "ocr"),
			filepath.Join(wd, "ocr"))
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "onnxruntime.dll")); err == nil {
			return dir
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "."
}

func (o *ortRuntime) call(slot int, args ...uintptr) uintptr {
	fn := *(*uintptr)(unsafe.Add(native(o.api), slot*8))
	r, _, _ := syscall.SyscallN(fn, args...)
	return r
}

// check turns a non-nil OrtStatus into an error and releases it.
func (o *ortRuntime) check(st uintptr, what string) error {
	if st == 0 {
		return nil
	}
	var msg string
	if p := o.call(ortSlotGetErrorMessage, st); p != 0 {
		msg = cString(p)
	}
	o.call(ortSlotReleaseStatus, st)
	return fmt.Errorf("ocr: onnxruntime %s: %s", what, msg)
}

type ortSession struct {
	rt         *ortRuntime
	ptr        uintptr
	inputName  string
	outputName string
	mu         sync.Mutex
}

func newORTSession(modelPath, inputName, outputName string) (*ortSession, error) {
	rt, err := sharedORT()
	if err != nil {
		return nil, err
	}
	var opts uintptr
	if err := rt.check(rt.call(ortSlotCreateSessionOptions, uintptr(unsafe.Pointer(&opts))), "CreateSessionOptions"); err != nil {
		return nil, err
	}
	if err := rt.check(rt.call(ortSlotSetIntraOpNumThreads, opts, 2), "SetIntraOpNumThreads"); err != nil {
		rt.call(ortSlotReleaseSessionOptions, opts)
		return nil, err
	}
	if err := rt.check(rt.call(ortSlotSetGraphOptimizationLevel, opts, ortOptimizeAll), "SetGraphOptimizationLevel"); err != nil {
		rt.call(ortSlotReleaseSessionOptions, opts)
		return nil, err
	}
	path, err := syscall.UTF16PtrFromString(modelPath)
	if err != nil {
		rt.call(ortSlotReleaseSessionOptions, opts)
		return nil, err
	}
	var sess uintptr
	if err := rt.check(rt.call(ortSlotCreateSession, rt.env, uintptr(unsafe.Pointer(path)), opts,
		uintptr(unsafe.Pointer(&sess))), "CreateSession"); err != nil {
		rt.call(ortSlotReleaseSessionOptions, opts)
		return nil, err
	}
	rt.call(ortSlotReleaseSessionOptions, opts)
	return &ortSession{rt: rt, ptr: sess, inputName: inputName, outputName: outputName}, nil
}

// run feeds one float32 tensor and returns the first output tensor.
func (s *ortSession) run(input []float32, shape []int64) ([]float32, []int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var in uintptr
	if err := s.rt.check(s.rt.call(ortSlotCreateTensorWithDataAsOrtValue, s.rt.memInfo,
		uintptr(unsafe.Pointer(&input[0])), uintptr(len(input)*4),
		uintptr(unsafe.Pointer(&shape[0])), uintptr(len(shape)), ortTensorFloat,
		uintptr(unsafe.Pointer(&in))), "CreateTensor"); err != nil {
		return nil, nil, err
	}
	defer s.rt.call(ortSlotReleaseValue, in)

	inName, err := syscall.BytePtrFromString(s.inputName)
	if err != nil {
		return nil, nil, err
	}
	outName, err := syscall.BytePtrFromString(s.outputName)
	if err != nil {
		return nil, nil, err
	}
	inNames := []uintptr{uintptr(unsafe.Pointer(inName))}
	inputs := []uintptr{in}
	outNames := []uintptr{uintptr(unsafe.Pointer(outName))}
	outputs := make([]uintptr, 1)
	if err := s.rt.check(s.rt.call(ortSlotRun, s.ptr, 0,
		uintptr(unsafe.Pointer(&inNames[0])), uintptr(unsafe.Pointer(&inputs[0])), 1,
		uintptr(unsafe.Pointer(&outNames[0])), 1,
		uintptr(unsafe.Pointer(&outputs[0]))), "Run"); err != nil {
		return nil, nil, err
	}
	out := outputs[0]
	defer s.rt.call(ortSlotReleaseValue, out)

	var info uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetTensorTypeAndShape, out,
		uintptr(unsafe.Pointer(&info))), "GetTensorTypeAndShape"); err != nil {
		return nil, nil, err
	}
	defer s.rt.call(ortSlotReleaseTensorTypeAndShape, info)

	var dimCount uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetDimensionsCount, info,
		uintptr(unsafe.Pointer(&dimCount))), "GetDimensionsCount"); err != nil {
		return nil, nil, err
	}
	dims := make([]int64, dimCount)
	if dimCount > 0 {
		if err := s.rt.check(s.rt.call(ortSlotGetDimensions, info,
			uintptr(unsafe.Pointer(&dims[0])), dimCount), "GetDimensions"); err != nil {
			return nil, nil, err
		}
	}
	var count uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetTensorShapeElementCount, info,
		uintptr(unsafe.Pointer(&count))), "GetTensorShapeElementCount"); err != nil {
		return nil, nil, err
	}
	var dataPtr uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetTensorMutableData, out,
		uintptr(unsafe.Pointer(&dataPtr))), "GetTensorMutableData"); err != nil {
		return nil, nil, err
	}
	result := make([]float32, count)
	if count > 0 {
		copy(result, unsafe.Slice((*float32)(native(dataPtr)), count))
	}
	return result, dims, nil
}

// cString reads a NUL-terminated byte string owned by the DLL.
func cString(p uintptr) string {
	n := 0
	for n < 1<<20 {
		if *(*byte)(native(p + uintptr(n))) == 0 {
			break
		}
		n++
	}
	return string(unsafe.Slice((*byte)(native(p)), n))
}

// native turns an address the DLL handed back into a pointer Go can look
// through; the round trip through its own address keeps go vet content,
// as in internal/selection and internal/capture.
func native(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
