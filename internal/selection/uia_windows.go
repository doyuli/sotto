//go:build windows

package selection

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
)

// The UI Automation path: ask the focused element for its TextPattern
// selection. Zero side effects — nothing touches the clipboard — and it
// names the selection's rectangle, which the clipboard path cannot.
//
// A frozen target app blocks the COM call, so every grab runs on a fresh
// MTA thread with a timeout that abandons it: a stuck call then costs
// one goroutine, not the app. The calls also go one at a time — two
// concurrent calls into the same provider, a timed-out grab's abandoned
// call and the next grab's fresh one, can hang each other — and
// consecutive timeouts open a circuit breaker that skips UIA for a
// while, so a bad neighbor does not add 800ms to every grab.

const (
	// Chromium UIA answers slowly on its first call, when the accessibility
	// tree is still warming up; 600ms timed out on grabs that would land.
	uiaTimeout         = 800 * time.Millisecond
	uiaBreakerAfter    = 3
	uiaBreakerCooldown = 60 * time.Second
	// How long a grab queues for the UIA slot before giving up on it. A
	// short wait rides out the normal overlap — the previous call just
	// finishing — but a call stuck inside the provider holds the slot
	// for good, and the clipboard beats queueing behind it.
	uiaBusyWait = 150 * time.Millisecond
)

// The internal outcomes grabUIA reports to Grab.
var (
	errUIAMiss    = errors.New("uia: no text pattern or selection")
	errUIASkipped = errors.New("uia: skipped (breaker open or a call in flight)")
	errUIATimeout = errors.New("uia: timeout")
)

// Vtable slots, counted from UIAutomationClient.h's interface orders
// (IUnknown's three first).
const (
	vtblRelease                    = 2
	vtblUiaGetFocusedElement       = 8
	vtblElementGetCurrentPatternAs = 14
	vtblTextPatternGetSelection    = 5
	vtblRangeArrayGetLength        = 3
	vtblRangeArrayGetElement       = 4
	vtblRangeGetBoundingRectangles = 10
	vtblRangeGetText               = 12
)

const uiaTextPatternID = 10014

var (
	clsidCUIAutomation = ole.NewGUID("{ff48dba4-60ef-4201-aa87-54103eef594e}")
	iidIUIAutomation   = ole.NewGUID("{30cbe57d-d9d0-452a-ab13-7ac5ac4825ee}")
	iidITextPattern    = ole.NewGUID("{32eba289-3583-42c9-9c59-3b6d9a1e9b6a}")
)

// uiaBreaker skips UIA for a while after consecutive timeouts.
type uiaBreaker struct {
	mu        sync.Mutex
	timeouts  int
	openUntil time.Time
}

var breaker uiaBreaker

// uiaMu admits one UIA call at a time; acquiring is a receive, like
// clipboardMu. The worker holds the slot until its COM call returns —
// even one the grab has already timed out on — so a stuck call keeps
// the slot and the next grab skips UIA instead of piling a second call
// onto the same provider. A call that never returns therefore disables
// UIA for the rest of the process; the clipboard path carries on.
var uiaMu = make(chan struct{}, 1)

func init() { uiaMu <- struct{}{} }

func (b *uiaBreaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return time.Now().After(b.openUntil)
}

func (b *uiaBreaker) noteTimeout() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timeouts++
	if b.timeouts >= uiaBreakerAfter {
		b.openUntil = time.Now().Add(uiaBreakerCooldown)
		b.timeouts = 0
	}
}

func (b *uiaBreaker) noteAnswer() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timeouts = 0
}

// grabUIA asks the focused element for its selection, through the
// single-slot, timeout-and-breaker wrapper.
func grabUIA(ctx context.Context) (Selection, error) {
	if !breaker.allow() {
		return Selection{}, errUIASkipped
	}
	select {
	case <-uiaMu:
	case <-time.After(uiaBusyWait):
		// The slot is taken by a call still inside the provider —
		// exactly the call a timeout has already abandoned once.
		return Selection{}, errUIASkipped
	}
	type answer struct {
		sel Selection
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() { uiaMu <- struct{}{} }()
		sel, err := uiaGrabThread()
		ch <- answer{sel, err}
	}()
	timer := time.NewTimer(uiaTimeout)
	defer timer.Stop()
	select {
	case a := <-ch:
		breaker.noteAnswer()
		return a.sel, a.err
	case <-timer.C:
		breaker.noteTimeout()
		return Selection{}, errUIATimeout
	case <-ctx.Done():
		return Selection{}, ctx.Err()
	}
}

// uiaGrabThread runs on its own thread: a UIA client must join the MTA,
// and a fresh thread per grab keeps a hung call from blocking the next.
func uiaGrabThread() (Selection, error) {
	// COINIT_MULTITHREADED = 0: the apartment UIA clients want, which
	// needs no message pump.
	if err := ole.CoInitializeEx(0, 0); err != nil {
		return Selection{}, fmt.Errorf("uia: CoInitializeEx: %w", err)
	}
	defer ole.CoUninitialize()

	unk, err := ole.CreateInstance(clsidCUIAutomation, iidIUIAutomation)
	if err != nil {
		return Selection{}, fmt.Errorf("uia: CoCreateInstance(CUIAutomation): %w", err)
	}
	auto := unsafe.Pointer(unk)
	defer comCall(auto, vtblRelease)

	var elem unsafe.Pointer
	if err := comCall(auto, vtblUiaGetFocusedElement, uintptr(unsafe.Pointer(&elem))); err != nil {
		return Selection{}, fmt.Errorf("uia: GetFocusedElement: %w", err)
	}
	if elem == nil {
		return Selection{}, errUIAMiss
	}
	defer comCall(elem, vtblRelease)

	var pattern unsafe.Pointer
	if err := comCall(elem, vtblElementGetCurrentPatternAs,
		uintptr(uiaTextPatternID), uintptr(unsafe.Pointer(iidITextPattern)),
		uintptr(unsafe.Pointer(&pattern))); err != nil || pattern == nil {
		return Selection{}, errUIAMiss // most apps simply have no text
	}
	defer comCall(pattern, vtblRelease)

	var ranges unsafe.Pointer
	if err := comCall(pattern, vtblTextPatternGetSelection, uintptr(unsafe.Pointer(&ranges))); err != nil || ranges == nil {
		return Selection{}, errUIAMiss
	}
	defer comCall(ranges, vtblRelease)

	var length int32
	if err := comCall(ranges, vtblRangeArrayGetLength, uintptr(unsafe.Pointer(&length))); err != nil || length == 0 {
		return Selection{}, errUIAMiss
	}

	sel := Selection{Via: "uia"}
	for i := int32(0); i < length && sel.Text == ""; i++ {
		var r unsafe.Pointer
		if err := comCall(ranges, vtblRangeArrayGetElement, uintptr(uint32(i)), uintptr(unsafe.Pointer(&r))); err != nil || r == nil {
			continue
		}
		text, ok := rangeText(r)
		if ok {
			sel.Text = text
			if rects, ok := rangeRects(r); ok && len(rects) >= 4 && sel.Text != "" {
				// The first range's rectangle places the bubble.
				if rect, ok := physRectToDIP(rects[0], rects[1], rects[2], rects[3]); ok {
					sel.Rect, sel.HasRect = rect, true
				}
			}
		}
		comCall(r, vtblRelease)
	}
	if strings.TrimSpace(sel.Text) == "" {
		// The pattern answered with selection ranges that hold no text.
		// Most apps mean "nothing selected", but some Chromium ones answer
		// this way while a selection exists, so grab decides whether the
		// simulated copy is worth trying.
		return Selection{}, nil
	}
	return sel, nil
}

// rangeText reads an ITextRange's text, or reports that it has none.
func rangeText(r unsafe.Pointer) (string, bool) {
	var bstr uintptr
	if err := comCall(r, vtblRangeGetText, ^uintptr(0), uintptr(unsafe.Pointer(&bstr))); err != nil || bstr == 0 {
		return "", false
	}
	text := bstrText(bstr)
	ole.SysFreeString((*int16)(native(bstr)))
	return text, text != ""
}

// rangeRects reads an ITextRange's bounding rectangles, an array of
// x, y, width, height quadruples in physical pixels.
func rangeRects(r unsafe.Pointer) ([]float64, bool) {
	var sa unsafe.Pointer
	if err := comCall(r, vtblRangeGetBoundingRectangles, uintptr(unsafe.Pointer(&sa))); err != nil || sa == nil {
		return nil, false
	}
	defer procSafeArrayDestroy.Call(uintptr(sa))
	var lb, ub int32
	if hr, _, _ := procSafeArrayGetLBound.Call(uintptr(sa), 1, uintptr(unsafe.Pointer(&lb))); hr != 0 {
		return nil, false
	}
	if hr, _, _ := procSafeArrayGetUBound.Call(uintptr(sa), 1, uintptr(unsafe.Pointer(&ub))); hr != 0 {
		return nil, false
	}
	if ub < lb {
		return nil, false
	}
	out := make([]float64, 0, ub-lb+1)
	for i := lb; i <= ub; i++ {
		var v float64
		if hr, _, _ := procSafeArrayGetElement.Call(uintptr(sa), uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&v))); hr != 0 {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// bstrText reads a BSTR as a Go string.
func bstrText(p uintptr) string {
	n := ole.SysStringLen((*int16)(native(p)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(native(p)), n))
}

// native turns a pointer Win32 returned into one Go can look through.
// The round trip through its own address keeps the conversion legal for
// go vet, which cannot know the uintptr held a pointer all along; the
// same trick mygo's own win32 layer uses.
func native(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

// comCall calls slot slot of the COM interface at obj, obj being the
// interface pointer itself. Errors carry the HRESULT.
func comCall(obj unsafe.Pointer, slot int, args ...uintptr) error {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*8))
	call := append([]uintptr{uintptr(obj)}, args...)
	hr, _, _ := syscall.SyscallN(fn, call...)
	if hr != 0 {
		return fmt.Errorf("HRESULT 0x%08x", int32(hr))
	}
	return nil
}
