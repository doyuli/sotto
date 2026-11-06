//go:build windows

package selection

import (
	"context"
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 plumbing for the selection paths: key synthesis, the raw
// clipboard, the foreground window, and the physical-to-DIP conversion
// that matches mygo's screen coordinates. Everything goes through
// LazyProcs so nothing loads until a grab needs it.

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modOleAut32 = windows.NewLazySystemDLL("oleaut32.dll")
	modShcore   = windows.NewLazySystemDLL("shcore.dll")

	procSendInput                  = modUser32.NewProc("SendInput")
	procGetAsyncKeyState           = modUser32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow        = modUser32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId   = modUser32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW              = modUser32.NewProc("GetClassNameW")
	procGetClipboardSequenceNumber = modUser32.NewProc("GetClipboardSequenceNumber")
	procOpenClipboard              = modUser32.NewProc("OpenClipboard")
	procCloseClipboard             = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard             = modUser32.NewProc("EmptyClipboard")
	procEnumClipboardFormats       = modUser32.NewProc("EnumClipboardFormats")
	procGetClipboardData           = modUser32.NewProc("GetClipboardData")
	procSetClipboardData           = modUser32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = modUser32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardOwner          = modUser32.NewProc("GetClipboardOwner")
	procRegisterClipboardFormatW   = modUser32.NewProc("RegisterClipboardFormatW")
	procEnumDisplayMonitors        = modUser32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW            = modUser32.NewProc("GetMonitorInfoW")
	procGetDpiForMonitor           = modShcore.NewProc("GetDpiForMonitor")

	procGlobalAlloc  = modKernel32.NewProc("GlobalAlloc")
	procGlobalFree   = modKernel32.NewProc("GlobalFree")
	procGlobalLock   = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock = modKernel32.NewProc("GlobalUnlock")
	procGlobalSize   = modKernel32.NewProc("GlobalSize")

	procSafeArrayGetLBound  = modOleAut32.NewProc("SafeArrayGetLBound")
	procSafeArrayGetUBound  = modOleAut32.NewProc("SafeArrayGetUBound")
	procSafeArrayGetElement = modOleAut32.NewProc("SafeArrayGetElement")
	procSafeArrayDestroy    = modOleAut32.NewProc("SafeArrayDestroy")
)

// Virtual-key codes and SendInput constants.
const (
	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkShift   = 0x10
	vkLWin    = 0x5B
	vkRWin    = 0x5C
	vkC       = 0x43
	vkInsert  = 0x2D

	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
	gmemMoveable   = 0x0002
	cfUnicodeText  = 13
)

// keyboardInput is SendInput's INPUT struct for a keyboard event, 40
// bytes on amd64: type, then the union (a KEYBDINPUT at its start,
// padded to the union's MOUSEINPUT width). The pads place every field
// exactly where Win32 expects it; a unit test pins the size.
type keyboardInput struct {
	Type    uint32
	pad1    uint32
	WVk     uint16
	WScan   uint16
	DwFlags uint32
	Time    uint32
	pad2    uint32
	Extra   uintptr
	pad3    [8]byte
}

// sendKeys sends a batch of key events in one SendInput call, which
// keeps them from interleaving with anything else.
func sendKeys(events ...keyboardInput) bool {
	if len(events) == 0 {
		return true
	}
	n, _, _ := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	return n == uintptr(len(events))
}

func keyEvent(vk uint16, up bool) keyboardInput {
	flags := uint32(0)
	if up {
		flags = keyeventfKeyUp
	}
	return keyboardInput{Type: inputKeyboard, WVk: vk, DwFlags: flags}
}

// copyBlockers are the modifiers that turn a plain Ctrl+C into
// something else: Ctrl+Alt+C dies in the window menu, Ctrl+Shift+C is
// another shortcut in half the apps, and a Win chord never copies. Ctrl
// itself is not among them — a copy sent while Ctrl is held is exactly
// the copy. The transaction still waits for Ctrl to avoid mistaking its
// later release for unrelated user input.
var copyBlockers = map[uint16]bool{
	vkMenu:  true, // Alt
	vkShift: true,
	vkLWin:  true,
	vkRWin:  true,
}

// waitCopyModifiers waits for the modifiers that would corrupt the copy
// to come up on their own. Ctrl is also awaited so its eventual release
// cannot be mistaken for new input while restoring the clipboard.
// Synthesizing their keyups looks equivalent but is
// not: an Alt keyup with no matching keydown puts the focused window
// into menu mode, and the Ctrl+C that follows is eaten by the menu —
// the copy then never lands, which made the clipboard path fail for
// every hotkey that carries Alt. Real key releases leave no such state
// behind, so the grab waits for them, and when they do not come it
// skips the copy rather than send a corrupted chord.
func waitCopyModifiers(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for anyKeyDown(copyBlockers) || anyKeyDown(map[uint16]bool{vkControl: true}) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return ErrShortcutHeld
		case <-tick.C:
		}
	}
	return ctx.Err()
}

// anyKeyDown reports whether any of the given virtual keys is held.
// GetAsyncKeyState's high bit is set while the key is down.
func anyKeyDown(keys map[uint16]bool) bool {
	for vk := range keys {
		if r, _, _ := procGetAsyncKeyState.Call(uintptr(vk)); int16(r) < 0 {
			return true
		}
	}
	return false
}

// copyChord is the key sequence a simulated copy sends. The modifiers go
// down in order and up in reverse.
type copyChord struct {
	modifiers []uint16
	key       uint16
}

// copyChordFor picks the copy chord for a window class, and reports whether
// a simulated copy is safe there at all. Ctrl+C is the copy in almost every
// app; the classic consoles use Ctrl+Insert. Windows Terminal has no safe
// chord: its Ctrl+Shift+C copies a selection but, with none, reaches the
// terminal as Ctrl+C and interrupts the running program. Its UIA answers a
// selection reliably, so skipping the copy costs nothing.
func copyChordFor(class string) (copyChord, bool) {
	switch class {
	case "ConsoleWindowClass", "mintty": // legacy conhost, Git Bash
		return copyChord{modifiers: []uint16{vkControl}, key: vkInsert}, true
	case "CASCADIA_HOSTING_WINDOW_CLASS": // Windows Terminal
		return copyChord{}, false
	default:
		return copyChord{modifiers: []uint16{vkControl}, key: vkC}, true
	}
}

// sendCopy sends one copy chord.
func sendCopy(chord copyChord, marker uintptr) bool {
	return sendKeys(copyKeyEvents(chord, marker, func(vk uint16) bool {
		return anyKeyDown(map[uint16]bool{vk: true})
	})...)
}

func copyKeyEvents(chord copyChord, marker uintptr, down func(uint16) bool) []keyboardInput {
	events := []keyboardInput{}
	// Do not release a modifier the user is physically holding.
	for _, mod := range chord.modifiers {
		if !down(mod) {
			events = append(events, keyEvent(mod, false))
		}
	}
	events = append(events, keyEvent(chord.key, false), keyEvent(chord.key, true))
	for i := len(events) - 3; i >= 0; i-- {
		events = append(events, keyEvent(events[i].WVk, true))
	}
	for i := range events {
		events[i].Extra = marker
	}
	return events
}

// foregroundWindow identifies the foreground window: its handle, the
// process id that owns it, and its window class, read as one snapshot.
func foregroundWindow() (hwnd uintptr, pid uint32, class string) {
	hwnd, _, _ = procGetForegroundWindow.Call()
	if hwnd == 0 {
		return 0, 0, ""
	}
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	var buf [128]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n > 0 && n < uintptr(len(buf)) {
		class = syscall.UTF16ToString(buf[:n])
	}
	return hwnd, pid, class
}

// clipboardSeq is the clipboard's change counter.
func clipboardSeq() uint32 {
	seq, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(seq)
}

// clipboardOwnerPID is the process that last wrote the clipboard, 0 when
// the clipboard has no owner window (as ours, opened without one).
func clipboardOwnerPID() uint32 {
	hwnd, _, _ := procGetClipboardOwner.Call()
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// openClipboard opens the clipboard, retrying: another process can hold
// it briefly, and a grab must not fail over that.
func openClipboard() bool { return openClipboardFor(0) }

func openClipboardFor(owner uintptr) bool {
	for attempt := 0; attempt < 10; attempt++ {
		ok, _, _ := procOpenClipboard.Call(owner)
		if ok != 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func closeClipboard() { procCloseClipboard.Call() }

// gdiClipboardFormats hold GDI handles rather than global memory, which
// a save cannot copy. Refuse simulated copy when any are present rather
// than silently dropping the user's original content.
var gdiClipboardFormats = map[uint16]bool{
	2:    true, // CF_BITMAP
	3:    true, // CF_METAFILEPICT
	9:    true, // CF_PALETTE
	14:   true, // CF_ENHMETAFILE
	0x80: true, // CF_OWNERDISPLAY
	0x82: true, // CF_DSPMETAFILEPICT
	0x8E: true, // CF_DSPENHMETAFILE
}

// saveClipboard copies every global-memory format off the clipboard,
// under one open. Returns nil if any format cannot be preserved or the
// snapshot exceeds the memory budget.
func saveClipboard() *clipboardSnapshot {
	if !openClipboard() {
		return nil
	}
	defer closeClipboard()
	snap := clipboardSnapshot{sequence: clipboardSeq()}
	var total uintptr
	for f := uintptr(0); ; {
		f, _, _ = procEnumClipboardFormats.Call(f)
		if f == 0 {
			break
		}
		id := uint16(f)
		if gdiClipboardFormats[id] {
			return nil
		}
		h, _, _ := procGetClipboardData.Call(f)
		if h == 0 {
			return nil
		}
		size, _, _ := procGlobalSize.Call(h)
		total += size
		if size == 0 || total > 32<<20 {
			return nil
		}
		p, _, _ := procGlobalLock.Call(h)
		if p == 0 {
			return nil
		}
		data := make([]byte, size)
		copy(data, unsafe.Slice((*byte)(native(p)), size))
		procGlobalUnlock.Call(h)
		snap.formats = append(snap.formats, clipboardFormat{id: id, data: data})
	}
	return &snap
}

// prepareClipboardRestore allocates every handle before simulated copy can
// replace the original. Successful SetClipboardData transfers its handle;
// only untransferred handles may be freed or retried.
type preparedClipboard struct {
	formats []clipboardFormat
	handles []uintptr
}

func prepareClipboardRestore(snap *clipboardSnapshot) (*preparedClipboard, error) {
	formats := append([]clipboardFormat(nil), snap.formats...)
	markers := []struct {
		name string
		data []byte
	}{
		{"CanIncludeInClipboardHistory", []byte{0, 0, 0, 0}},
		{"CanUploadToCloudClipboard", []byte{0, 0, 0, 0}},
		{"ExcludeClipboardContentFromMonitorProcessing", []byte{1}},
	}
	var protected []clipboardFormat
	for _, marker := range markers {
		name, _ := syscall.UTF16PtrFromString(marker.name)
		id, _, err := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
		if id == 0 {
			return nil, fmt.Errorf("register clipboard protection: %w", err)
		}
		protected = append(protected, clipboardFormat{id: uint16(id), data: marker.data})
		filtered := formats[:0]
		for _, f := range formats {
			if f.id != uint16(id) {
				filtered = append(filtered, f)
			}
		}
		formats = filtered
	}
	p := &preparedClipboard{formats: append(protected, formats...)}
	p.handles = make([]uintptr, len(p.formats))
	for i, f := range p.formats {
		h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(f.data)))
		if h == 0 {
			p.free()
			return nil, fmt.Errorf("allocate clipboard format: %w", err)
		}
		p.handles[i] = h
		address, _, err := procGlobalLock.Call(h)
		if address == 0 {
			p.free()
			return nil, fmt.Errorf("lock clipboard format: %w", err)
		}
		copy(unsafe.Slice((*byte)(native(address)), len(f.data)), f.data)
		procGlobalUnlock.Call(h)
	}
	return p, nil
}

func (p *preparedClipboard) free() {
	for i, h := range p.handles {
		if h != 0 {
			procGlobalFree.Call(h)
			p.handles[i] = 0
		}
	}
}

// Caller pins the OS thread through Open/CloseClipboard and owns a real HWND.
// A changed clipboard is deliberately untouched; all other failures are surfaced.
func (p *preparedClipboard) restore(owner uintptr, sequence uint32, unchanged func() bool) (restoreOutcome, uint32, error) {
	if owner == 0 {
		return restoreFailed, sequence, fmt.Errorf("clipboard owner missing: %w", ErrClipboardRestore)
	}
	if !openClipboardFor(owner) {
		return restoreFailed, sequence, fmt.Errorf("open clipboard: %w", ErrClipboardRestore)
	}
	defer closeClipboard()
	outcome, err := restoreClipboardFormats(len(p.formats), 3, unchanged, func() error {
		if ok, _, err := procEmptyClipboard.Call(); ok == 0 {
			return fmt.Errorf("empty clipboard: %w", err)
		}
		return nil
	}, func(i int) error {
		if ok, _, err := procSetClipboardData.Call(uintptr(p.formats[i].id), p.handles[i]); ok == 0 {
			return fmt.Errorf("restore clipboard format %d: %w", p.formats[i].id, err)
		}
		p.handles[i] = 0
		return nil
	})
	return outcome, clipboardSeq(), err
}

// clipboardHasText reports whether the clipboard holds text.
func clipboardHasText() bool {
	ok, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText)
	return ok != 0
}

// readClipboardText returns the clipboard's Unicode text. Windows
// synthesizes it from the older text formats, so one format covers all.
func readClipboardText() (string, bool) {
	if !openClipboard() {
		return "", false
	}
	defer closeClipboard()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	size, _, _ := procGlobalSize.Call(h)
	if size < 2 {
		return "", false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)
	runes := unsafe.Slice((*uint16)(native(p)), size/2)
	return syscall.UTF16ToString(runes), true
}

// A monitor, in the physical pixels Win32 reports, with its DPI.
type monitor struct {
	physLeft, physTop     int
	physRight, physBottom int
	dpi                   int
}

type monitorInfoEx struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	dwFlags   uint32
	szDevice  [32]uint16
}

type rect struct{ Left, Top, Right, Bottom int32 }

// monitorSink and monitorEnumProc: the enumeration callback is made
// once — Windows keeps a small pool of them — and the sink it feeds is
// guarded, as grabs can run concurrently.
var (
	monitorsMu    sync.Mutex
	monitorSink   func(uintptr)
	monitorEnumCb = syscall.NewCallback(func(mon, _, _, _ uintptr) uintptr {
		if monitorSink != nil {
			monitorSink(mon)
		}
		return 1
	})
)

// monitors enumerates the displays, in physical pixels with their DPI.
func monitors() []monitor {
	monitorsMu.Lock()
	defer monitorsMu.Unlock()
	var out []monitor
	monitorSink = func(mon uintptr) {
		var info monitorInfoEx
		info.cbSize = uint32(unsafe.Sizeof(info))
		if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&info))); r == 0 {
			return
		}
		m := monitor{
			physLeft:   int(info.rcMonitor.Left),
			physTop:    int(info.rcMonitor.Top),
			physRight:  int(info.rcMonitor.Right),
			physBottom: int(info.rcMonitor.Bottom),
		}
		m.dpi = 96
		if procGetDpiForMonitor.Find() == nil {
			var dx, dy uint32
			if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx != 0 {
				m.dpi = int(dx)
			}
		}
		out = append(out, m)
	}
	procEnumDisplayMonitors.Call(0, 0, monitorEnumCb, 0)
	monitorSink = nil
	return out
}

// toDIP is mygo's own conversion (internal/windows toDIP), so selection
// rects land in the coordinates its windows place.
func toDIP(v float64, dpi int) int {
	return int((v*96 + float64(dpi)/2) / float64(dpi))
}

// monitorAt returns the display containing a physical point, the last
// one when the point is off them all.
func monitorAt(x, y float64) monitor {
	var best monitor
	for _, m := range monitors() {
		best = m
		if x >= float64(m.physLeft) && x < float64(m.physRight) &&
			y >= float64(m.physTop) && y < float64(m.physBottom) {
			return m
		}
	}
	return best
}

// physRectToDIP converts a physical-pixel rect to mygo's DIP
// coordinates: mygo sizes each display by that display's own DPI, so
// the point's display decides the scale.
func physRectToDIP(x, y, w, h float64) (Rect, bool) {
	if w <= 0 || h <= 0 {
		return Rect{}, false
	}
	m := monitorAt(x, y)
	return Rect{
		X: toDIP(x, m.dpi),
		Y: toDIP(y, m.dpi),
		W: toDIP(w, m.dpi),
		H: toDIP(h, m.dpi),
	}, true
}
