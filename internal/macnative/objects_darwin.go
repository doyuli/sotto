//go:build darwin

// Package macnative owns the small public macOS API bridge used by Sotto.
// AppKit window operations belong to the main thread. Returned byte slices and
// strings own their memory; Objective-C objects stay inside an autorelease pool.
package macnative

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var frameworks sync.Map
var selectors sync.Map

func Framework(name string) (uintptr, error) {
	loader, _ := frameworks.LoadOrStore(name, sync.OnceValues(func() (uintptr, error) {
		path := "/System/Library/Frameworks/" + name + ".framework/" + name
		lib, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return 0, fmt.Errorf("load %s: %w", name, err)
		}
		return lib, nil
	}))
	return loader.(func() (uintptr, error))()
}

type Symbol struct {
	Target any
	Name   string
}

func Bind(framework string, symbols ...Symbol) error {
	lib, err := Framework(framework)
	if err != nil {
		return err
	}
	for _, s := range symbols {
		addr, err := purego.Dlsym(lib, s.Name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", s.Name, err)
		}
		purego.RegisterFunc(s.Target, addr)
	}
	return nil
}

func Sel(name string) objc.SEL {
	if s, ok := selectors.Load(name); ok {
		return s.(objc.SEL)
	}
	s, _ := selectors.LoadOrStore(name, objc.RegisterName(name))
	return s.(objc.SEL)
}

func Class(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

// Pool pins the autorelease pool to the thread that creates it. Foundation
// must have been loaded by the operation's initializer before entering here.
func Pool(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	p := Class("NSAutoreleasePool").Send(Sel("alloc")).Send(Sel("init"))
	defer p.Send(Sel("drain"))
	fn()
}

func String(s string) objc.ID {
	b := []byte(s)
	var ptr unsafe.Pointer
	if len(b) != 0 {
		ptr = unsafe.Pointer(&b[0])
	}
	v := Class("NSString").Send(Sel("alloc")).Send(Sel("initWithBytes:length:encoding:"), ptr, uint64(len(b)), uint64(4))
	v.Send(Sel("autorelease"))
	runtime.KeepAlive(b)
	return v
}

func GoString(v objc.ID) string {
	if v == 0 {
		return ""
	}
	n := objc.Send[uint64](v, Sel("lengthOfBytesUsingEncoding:"), uint64(4))
	if n == 0 || n > 32<<20 {
		return ""
	}
	p := objc.Send[unsafe.Pointer](v, Sel("UTF8String"))
	if p == nil {
		return ""
	}
	return string(unsafe.Slice((*byte)(p), int(n)))
}

func Data(b []byte) objc.ID {
	var ptr unsafe.Pointer
	if len(b) != 0 {
		ptr = unsafe.Pointer(&b[0])
	}
	v := Class("NSData").Send(Sel("dataWithBytes:length:"), ptr, uint64(len(b)))
	runtime.KeepAlive(b)
	return v
}

func Bytes(v objc.ID, limit int) ([]byte, error) {
	if v == 0 {
		return nil, fmt.Errorf("native data unavailable")
	}
	n := objc.Send[uint64](v, Sel("length"))
	if n > uint64(max(limit, 0)) {
		return nil, fmt.Errorf("native data exceeds %d byte budget", limit)
	}
	if n == 0 {
		return []byte{}, nil
	}
	p := objc.Send[unsafe.Pointer](v, Sel("bytes"))
	if p == nil {
		return nil, fmt.Errorf("native data has no bytes")
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), int(n))...), nil
}

func Array(values ...objc.ID) objc.ID {
	a := Class("NSMutableArray").Send(Sel("array"))
	for _, v := range values {
		a.Send(Sel("addObject:"), v)
	}
	return a
}

func Items(array objc.ID) []objc.ID {
	n := objc.Send[uint64](array, Sel("count"))
	if n > 100000 {
		return nil
	}
	out := make([]objc.ID, int(n))
	for i := range out {
		out[i] = array.Send(Sel("objectAtIndex:"), uint64(i))
	}
	return out
}

func NativeError(op string, err objc.ID) error {
	if err == 0 {
		return fmt.Errorf("%s failed", op)
	}
	return fmt.Errorf("%s: %s", op, GoString(err.Send(Sel("localizedDescription"))))
}
