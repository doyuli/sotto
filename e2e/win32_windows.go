//go:build e2e && windows

package e2e

import (
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Win32 helpers the smoke test needs: key synthesis, window
// enumeration, layered-window checks, and a dummy window to take the
// focus with.

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	procSendInput                  = modUser32.NewProc("SendInput")
	procGetForegroundWindow        = modUser32.NewProc("GetForegroundWindow")
	procSetForegroundWindow        = modUser32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId   = modUser32.NewProc("GetWindowThreadProcessId")
	procEnumWindows                = modUser32.NewProc("EnumWindows")
	procEnumChildWindows           = modUser32.NewProc("EnumChildWindows")
	procGetClassNameW              = modUser32.NewProc("GetClassNameW")
	procIsWindowVisible            = modUser32.NewProc("IsWindowVisible")
	procGetWindowLongW             = modUser32.NewProc("GetWindowLongW")
	procGetLayeredWindowAttributes = modUser32.NewProc("GetLayeredWindowAttributes")
	procCreateWindowExW            = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow              = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW             = modUser32.NewProc("DefWindowProcW")
	procRegisterClassExW           = modUser32.NewProc("RegisterClassExW")
	procAllowSetForegroundWindow   = modUser32.NewProc("AllowSetForegroundWindow")

	procGetModuleHandleW      = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procDwmGetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
)

const (
	inputKeyboard           = 1
	keyeventfKeyUp          = 0x0002
	gwlExStyle              = uintptr(0xFFFFFFEC) // -20 as an index
	wsExLayered             = 0x00080000
	wsExNoRedirectionBitmap = 0x00200000
	lwaAlpha                = 2
)

// Modifier virtual keys, by the names in a mygo accelerator.
var modVKs = map[string]uint16{
	"Ctrl":  0x11,
	"Alt":   0x12,
	"Shift": 0x10,
	"Win":   0x5B,
}

// keyboardInput mirrors INPUT for a keyboard event; 40 bytes on amd64,
// as internal/selection pins down.
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

// tapAccel presses a mygo accelerator ("Ctrl+Alt+A") down and up, so the
// test presses whatever the app actually registered rather than a
// hard-coded chord.
func tapAccel(accel string) {
	var mods []uint16
	vk := uint16(0)
	for _, part := range strings.Split(accel, "+") {
		if m, ok := modVKs[part]; ok {
			mods = append(mods, m)
			continue
		}
		vk = keyVK(part)
	}
	var events []keyboardInput
	for _, m := range mods {
		events = append(events, keyboardInput{Type: inputKeyboard, WVk: m})
	}
	events = append(events, keyboardInput{Type: inputKeyboard, WVk: vk})
	events = append(events, keyboardInput{Type: inputKeyboard, WVk: vk, DwFlags: keyeventfKeyUp})
	for i := len(mods) - 1; i >= 0; i-- {
		events = append(events, keyboardInput{Type: inputKeyboard, WVk: mods[i], DwFlags: keyeventfKeyUp})
	}
	procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
}

// keyVK converts an accelerator's final key name to a virtual-key code.
func keyVK(name string) uint16 {
	switch {
	case len(name) == 1 && name >= "A" && name <= "Z":
		return uint16(name[0])
	case len(name) == 1 && name >= "0" && name <= "9":
		return uint16(name[0])
	case len(name) > 1 && name[0] == 'F':
		var n int
		fmt.Sscanf(name[1:], "%d", &n)
		if n >= 1 && n <= 12 {
			return uint16(0x70 + n - 1)
		}
	case name == "Space":
		return 0x20
	case name == "Escape":
		return 0x1B
	}
	return 0
}

// foregroundPID is the process owning the foreground window.
func foregroundPID() uint32 {
	h, _, _ := procGetForegroundWindow.Call()
	if h == 0 {
		return 0
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// allowForeground lets pid take the foreground despite the synthetic
// input, the way a real user's keystroke would entitle it.
func allowForeground(pid uint32) {
	procAllowSetForegroundWindow.Call(uintptr(pid))
}

// activateOwnWindow creates a bare top-level window and puts it in the
// foreground, so the app under test loses the focus. It returns a close
// function.
func activateOwnWindow() (close func(), err error) {
	className, err := syscallUTF16("sotto-e2e-dummy")
	if err != nil {
		return nil, err
	}
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	var wc wndClassEx
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = defaultWndProc()
	wc.HInstance = hInstance
	wc.LpszClassName = uintptr(unsafe.Pointer(&className[0]))
	atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return nil, fmtLastError("RegisterClassExW")
	}
	name, _ := syscallUTF16("sotto-e2e")
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(&className[0])), uintptr(unsafe.Pointer(&name[0])),
		0, // overlapped window
		0, 0, 160, 90, 0, 0, hInstance, 0)
	if hwnd == 0 {
		return nil, fmtLastError("CreateWindowExW")
	}
	procSetForegroundWindow.Call(hwnd)
	return func() { procDestroyWindow.Call(hwnd) }, nil
}

// wndClassEx mirrors WNDCLASSEXW on amd64.
type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  uintptr
	LpszClassName uintptr
	HIconSm       uintptr
}

// defaultWndProc returns a callback that just passes through to
// DefWindowProcW.
func defaultWndProc() uintptr {
	return syscall.NewCallback(func(hwnd, msg, w, l uintptr) uintptr {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, w, l)
		return r
	})
}

// syscallUTF16 encodes s, NUL-terminated.
func syscallUTF16(s string) ([]uint16, error) {
	return windows.UTF16FromString(s)
}

// Win32 retains callback addresses, so polling reuses these callbacks.
var (
	enumerationMu  sync.Mutex
	windowSink     func(uintptr)
	childSink      func(uintptr)
	windowCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if windowSink != nil {
			windowSink(hwnd)
		}
		return 1
	})
	childCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if childSink != nil {
			childSink(hwnd)
		}
		return 1
	})
)

func visibleHandles(pid uint32) []uintptr {
	enumerationMu.Lock()
	defer enumerationMu.Unlock()
	var handles []uintptr
	windowSink = func(hwnd uintptr) {
		var owner uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			return
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return
		}
		handles = append(handles, hwnd)
	}
	procEnumWindows.Call(windowCallback, 0)
	windowSink = nil
	return handles
}

// A visible snip must have native content and no WebView child. DWM cloaking
// also has to end: IsWindowVisible alone accepts an unpresented native frame.
func nativeSnipReady(pid uint32) ([]uintptr, bool) {
	handles := visibleHandles(pid)
	if len(handles) == 0 {
		return nil, false
	}
	for _, hwnd := range handles {
		var cloaked uint32
		if status, _, _ := procDwmGetWindowAttribute.Call(hwnd, 14, uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked)); status != 0 || cloaked != 0 {
			return handles, false
		}
		native, web := false, false
		enumerationMu.Lock()
		childSink = func(child uintptr) {
			var name [256]uint16
			procGetClassNameW.Call(child, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
			class := windows.UTF16ToString(name[:])
			native = native || class == "MyGoSurface"
			web = web || strings.Contains(class, "WebView") || strings.Contains(class, "Chrome_WidgetWin")
		}
		procEnumChildWindows.Call(hwnd, childCallback, 0)
		childSink = nil
		enumerationMu.Unlock()
		if !native || web {
			return handles, false
		}
	}
	return handles, true
}

func rightClick() {
	type mouseInput struct {
		Type, pad               uint32
		X, Y                    int32
		Data, Flags, Time, pad2 uint32
		Extra                   uintptr
	}
	events := []mouseInput{{Flags: 0x0008}, {Flags: 0x0010}}
	procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
}

// fmtLastError names a failed call.
func fmtLastError(call string) error {
	return fmt.Errorf("e2e: %s failed", call)
}
