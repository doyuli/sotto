//go:build windows

package ocr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"sotto/internal/ocr/pipeline"

	"golang.org/x/sys/windows"
)

// A thin, cgo-free wrapper over the ONNX Runtime C API. The project is
// built with CGO_ENABLED=0; the function table ABI is fixed to API 31.

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
	ortSlotGetTensorElementType           = 60
	ortSlotGetDimensionsCount             = 61
	ortSlotGetDimensions                  = 62
	ortSlotGetTensorShapeElementCount     = 64
	ortSlotGetTensorTypeAndShape          = 65
	ortSlotCreateCpuMemoryInfo            = 69
	ortSlotReleaseEnv                     = 92
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
	ortMu sync.Mutex
	ortRT *ortRuntime
)

// Cache only successful initialization so repairing runtime files can recover
// on the next request, just like repairing model assets.
func sharedORT() (*ortRuntime, error) {
	ortMu.Lock()
	defer ortMu.Unlock()
	if ortRT != nil {
		return ortRT, nil
	}
	rt, err := loadORT()
	if err != nil {
		return nil, err
	}
	ortRT = rt
	return rt, nil
}

func loadORT() (*ortRuntime, error) {
	_, dll := ocrResourcePaths()
	if _, err := os.Stat(dll); err != nil {
		return nil, fmt.Errorf("ocr: stat %s: %w", dll, err)
	}
	h, err := windows.LoadLibraryEx(dll, 0, ortLoadLibraryFlags)
	if err != nil {
		// Older loaders reject the flag combination; the plain call
		// still resolves onnxruntime_providers_shared.dll when it sits
		// beside the executable or in the resource directory on PATH.
		h, err = windows.LoadLibrary(dll)
		if err != nil {
			return nil, fmt.Errorf("ocr: load onnxruntime: %w", err)
		}
	}
	proc, err := windows.GetProcAddress(h, "OrtGetApiBase")
	loaded := false
	defer func() {
		if !loaded {
			_ = windows.FreeLibrary(h)
		}
	}()
	if err != nil {
		return nil, fmt.Errorf("ocr: OrtGetApiBase: %w", err)
	}
	base, _, _ := syscall.SyscallN(proc)
	if base == 0 {
		return nil, fmt.Errorf("ocr: OrtGetApiBase returned nil")
	}
	// OrtApiBase is a struct of two function pointers; GetApi is first.
	getAPI := *(*uintptr)(native(base))
	api, _, _ := syscall.SyscallN(getAPI, ortAPIVersion)
	if api == 0 {
		return nil, fmt.Errorf("ocr: onnxruntime API %d unavailable", ortAPIVersion)
	}
	rt := &ortRuntime{lib: h, api: api}

	logid, _ := syscall.BytePtrFromString("sotto")
	var env uintptr
	if err := rt.check(rt.call(ortSlotCreateEnv, ortLoggingError,
		uintptr(unsafe.Pointer(logid)), uintptr(unsafe.Pointer(&env))), "CreateEnv"); err != nil {
		return nil, err
	}
	rt.env = env
	defer func() {
		if !loaded {
			rt.call(ortSlotReleaseEnv, env)
		}
	}()

	var mem uintptr
	if err := rt.check(rt.call(ortSlotCreateCpuMemoryInfo,
		ortDeviceAllocator, ortMemTypeDefault, uintptr(unsafe.Pointer(&mem))), "CreateCpuMemoryInfo"); err != nil {
		return nil, err
	}
	rt.memInfo = mem
	loaded = true
	return rt, nil
}

// ocrResourcePaths returns the model directory and runtime DLL path.
// MyGo merges both into ocr/ when packaging. A source checkout keeps
// the DLLs separate so each build gets only its own architecture.
func ocrResourcePaths() (modelDir, dll string) {
	if dir := os.Getenv("SOTTO_OCR_DIR"); dir != "" {
		return dir, filepath.Join(dir, "onnxruntime.dll")
	}
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
		if root := filepath.Dir(dir); filepath.Base(root) == "resources" {
			platformDLL := filepath.Join(root, "windows-"+runtime.GOARCH, "ocr", "onnxruntime.dll")
			if _, err := os.Stat(platformDLL); err == nil {
				return dir, platformDLL
			}
		}
		dll := filepath.Join(dir, "onnxruntime.dll")
		if _, err := os.Stat(dll); err == nil {
			return dir, dll
		}
	}
	if len(candidates) > 0 {
		return candidates[0], filepath.Join(candidates[0], "onnxruntime.dll")
	}
	return ".", "onnxruntime.dll"
}

// Arguments may point to Go storage consumed by the synchronous native call.
//
//go:uintptrescapes
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
	return newORTSessionWithThreads(modelPath, inputName, outputName, 2)
}

func newORTSessionWithThreads(modelPath, inputName, outputName string, threads int) (*ortSession, error) {
	if threads < 1 || threads > 16 {
		return nil, fmt.Errorf("ocr: invalid inference thread count: %d", threads)
	}
	rt, err := sharedORT()
	if err != nil {
		return nil, err
	}
	var opts uintptr
	if err := rt.check(rt.call(ortSlotCreateSessionOptions, uintptr(unsafe.Pointer(&opts))), "CreateSessionOptions"); err != nil {
		return nil, err
	}
	if err := rt.check(rt.call(ortSlotSetIntraOpNumThreads, opts, uintptr(threads)), "SetIntraOpNumThreads"); err != nil {
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

func (s *ortSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != 0 {
		s.rt.call(ortSlotReleaseSession, s.ptr)
		s.ptr = 0
	}
}

func (s *ortSession) Run(ctx context.Context, input pipeline.Tensor) (pipeline.Tensor, error) {
	if err := ctx.Err(); err != nil {
		return pipeline.Tensor{}, err
	}
	data, shape, err := s.run(input.Data, input.Shape)
	if canceled := ctx.Err(); canceled != nil {
		return pipeline.Tensor{}, canceled
	}
	return pipeline.Tensor{Data: data, Shape: shape}, err
}

// run feeds one float32 tensor and returns the first output tensor.
func (s *ortSession) run(input []float32, shape []int64) ([]float32, []int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr == 0 {
		return nil, nil, fmt.Errorf("ocr: session closed")
	}
	n, err := tensorElements(shape)
	if err != nil {
		return nil, nil, err
	}
	if len(input) != n {
		return nil, nil, fmt.Errorf("ocr: input shape/data mismatch")
	}
	// The DLL borrows these buffers until Run returns; uintptr arguments
	// alone do not keep their backing storage reachable to the GC.
	defer func() { runtime.KeepAlive(input); runtime.KeepAlive(shape) }()

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
	defer func() { runtime.KeepAlive(inName); runtime.KeepAlive(outName) }()
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
	if out == 0 {
		return nil, nil, fmt.Errorf("ocr: missing output tensor")
	}
	defer s.rt.call(ortSlotReleaseValue, out)

	var info uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetTensorTypeAndShape, out,
		uintptr(unsafe.Pointer(&info))), "GetTensorTypeAndShape"); err != nil {
		return nil, nil, err
	}
	defer s.rt.call(ortSlotReleaseTensorTypeAndShape, info)
	var elementType int32
	if err := s.rt.check(s.rt.call(ortSlotGetTensorElementType, info,
		uintptr(unsafe.Pointer(&elementType))), "GetTensorElementType"); err != nil {
		return nil, nil, err
	}
	if elementType != ortTensorFloat {
		return nil, nil, fmt.Errorf("ocr: output is not float32")
	}

	var dimCount uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetDimensionsCount, info,
		uintptr(unsafe.Pointer(&dimCount))), "GetDimensionsCount"); err != nil {
		return nil, nil, err
	}
	if dimCount == 0 || dimCount > 4 {
		return nil, nil, fmt.Errorf("ocr: invalid output rank %d", dimCount)
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
	expected, err := tensorElements(dims)
	if err != nil {
		return nil, nil, err
	}
	if count != uintptr(expected) {
		return nil, nil, fmt.Errorf("ocr: output shape/count mismatch")
	}
	var dataPtr uintptr
	if err := s.rt.check(s.rt.call(ortSlotGetTensorMutableData, out,
		uintptr(unsafe.Pointer(&dataPtr))), "GetTensorMutableData"); err != nil {
		return nil, nil, err
	}
	if dataPtr == 0 || dataPtr%4 != 0 {
		return nil, nil, fmt.Errorf("ocr: invalid output data pointer")
	}
	result := make([]float32, expected)
	copy(result, unsafe.Slice((*float32)(native(dataPtr)), expected))
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
// through without an intermediate integer-to-pointer conversion.
func native(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}
