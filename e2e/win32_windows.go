//go:build e2e && windows

package e2e

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Win32 helpers the smoke test needs: key synthesis, window
// enumeration, layered-window checks, and a dummy window to take the
// focus with.

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	procSendInput                     = modUser32.NewProc("SendInput")
	procGetForegroundWindow           = modUser32.NewProc("GetForegroundWindow")
	procGetCursorPos                  = modUser32.NewProc("GetCursorPos")
	procSetCursorPos                  = modUser32.NewProc("SetCursorPos")
	procGetWindowRect                 = modUser32.NewProc("GetWindowRect")
	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGetThreadDpiAwarenessContext  = modUser32.NewProc("GetThreadDpiAwarenessContext")
	procAreDpiAwarenessContextsEqual  = modUser32.NewProc("AreDpiAwarenessContextsEqual")
	procSetForegroundWindow           = modUser32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId      = modUser32.NewProc("GetWindowThreadProcessId")
	procEnumWindows                   = modUser32.NewProc("EnumWindows")
	procEnumChildWindows              = modUser32.NewProc("EnumChildWindows")
	procGetClassNameW                 = modUser32.NewProc("GetClassNameW")
	procIsWindowVisible               = modUser32.NewProc("IsWindowVisible")
	procGetWindowLongW                = modUser32.NewProc("GetWindowLongW")
	procGetLayeredWindowAttributes    = modUser32.NewProc("GetLayeredWindowAttributes")
	procCreateWindowExW               = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow                 = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW                = modUser32.NewProc("DefWindowProcW")
	procRegisterClassExW              = modUser32.NewProc("RegisterClassExW")
	procUnregisterClassW              = modUser32.NewProc("UnregisterClassW")
	procPeekMessageW                  = modUser32.NewProc("PeekMessageW")
	procTranslateMessage              = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW              = modUser32.NewProc("DispatchMessageW")
	procAllowSetForegroundWindow      = modUser32.NewProc("AllowSetForegroundWindow")

	procGetModuleHandleW      = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procDwmGetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
)

const (
	inputKeyboard           = 1
	keyeventfKeyUp          = 0x0002
	gwlExStyle              = uintptr(0xFFFFFFEC) // -20 as an index
	wsExLayered             = 0x00080000
	wsExNoRedirectionBitmap = 0x00200000
	wsVisible               = 0x10000000
	pmRemove                = 0x0001
	lwaAlpha                = 2
	dpiPerMonitorAwareV2    = ^uintptr(0) - 3 // -4 as an opaque DPI context
)

func setTestDPIAwareness() error {
	if err := procSetProcessDpiAwarenessContext.Find(); err != nil {
		return fmt.Errorf("e2e: per-monitor DPI API unavailable: %w", err)
	}
	ok, _, err := procSetProcessDpiAwarenessContext.Call(dpiPerMonitorAwareV2)
	if ok != 0 {
		return nil
	}
	// A manifest may have set the correct context already. Accept that case,
	// but never run coordinate assertions under an unknown DPI context.
	if procGetThreadDpiAwarenessContext.Find() == nil && procAreDpiAwarenessContextsEqual.Find() == nil {
		current, _, _ := procGetThreadDpiAwarenessContext.Call()
		if equal, _, _ := procAreDpiAwarenessContextsEqual.Call(current, dpiPerMonitorAwareV2); equal != 0 {
			return nil
		}
	}
	return fmt.Errorf("e2e: set per-monitor DPI awareness: %w", err)
}

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

// activateOwnWindow keeps a responsive foreground target for the selection
// hotkey. Its owning thread also activates and destroys it.
func activateOwnWindow() (func(), error) {
	ready := make(chan error, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		cleanup, err := createForegroundWindow()
		ready <- err
		if err != nil {
			return
		}
		defer cleanup()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var msg windowMessage
		msgPtr := unsafe.Pointer(&msg)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				for {
					ok, _, _ := procPeekMessageW.Call(uintptr(msgPtr), 0, 0, 0, pmRemove)
					if ok == 0 {
						break
					}
					procTranslateMessage.Call(uintptr(msgPtr))
					procDispatchMessageW.Call(uintptr(msgPtr))
				}
			}
		}
	}()
	if err := <-ready; err != nil {
		<-done
		return nil, err
	}
	return sync.OnceFunc(func() {
		close(stop)
		<-done
	}), nil
}

func createForegroundWindow() (func(), error) {
	className, err := syscallUTF16("sotto-e2e-dummy")
	if err != nil {
		return nil, err
	}
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	var wc wndClassEx
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = defaultWndProc
	wc.HInstance = hInstance
	wc.LpszClassName = uintptr(unsafe.Pointer(&className[0]))
	atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return nil, fmtLastError("RegisterClassExW")
	}
	unregister := func() { procUnregisterClassW.Call(uintptr(unsafe.Pointer(&className[0])), hInstance) }
	name, _ := syscallUTF16("sotto-e2e")
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(&className[0])), uintptr(unsafe.Pointer(&name[0])),
		wsVisible, // visible overlapped window
		0, 0, 160, 90, 0, 0, hInstance, 0)
	if hwnd == 0 {
		unregister()
		return nil, fmtLastError("CreateWindowExW")
	}
	cleanup := func() {
		procDestroyWindow.Call(hwnd)
		unregister()
	}
	procSetForegroundWindow.Call(hwnd)
	if foreground, _, _ := procGetForegroundWindow.Call(); foreground != hwnd {
		cleanup()
		return nil, fmt.Errorf("e2e: dummy window did not get foreground focus")
	}
	return cleanup, nil
}

// windowMessage mirrors Win32 MSG.
type windowMessage struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
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

var defaultWndProc = syscall.NewCallback(func(hwnd, msg, w, l uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, w, l)
	return r
})

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

type mouseInput struct {
	Type, pad               uint32
	X, Y                    int32
	Data, Flags, Time, pad2 uint32
	Extra                   uintptr
}

func sendMouse(events ...mouseInput) bool {
	n, _, _ := procSendInput.Call(uintptr(len(events)), uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	return n == uintptr(len(events))
}

func rightClick() {
	sendMouse(mouseInput{Flags: 0x0008}, mouseInput{Flags: 0x0010})
}

// Leave the primary button held until Escape cancels, so this regression
// exercises a painted selection without starting OCR on the test desktop.
func beginSnipDrag(hwnd uintptr) (release func(), err error) {
	var bounds struct{ Left, Top, Right, Bottom int32 }
	var cursor struct{ X, Y int32 }
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&bounds))); ok == 0 {
		return nil, fmtLastError("GetWindowRect")
	}
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor))); ok == 0 {
		return nil, fmtLastError("GetCursorPos")
	}
	release = sync.OnceFunc(func() {
		sendMouse(mouseInput{Flags: 0x0004})
		procSetCursorPos.Call(uintptr(cursor.X), uintptr(cursor.Y))
	})
	x, y := (bounds.Left+bounds.Right)/2, (bounds.Top+bounds.Bottom)/2
	if ok, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y)); ok == 0 {
		return nil, fmtLastError("SetCursorPos")
	}
	if !sendMouse(mouseInput{Flags: 0x0002}) {
		release()
		return nil, fmtLastError("SendInput")
	}
	if ok, _, _ := procSetCursorPos.Call(uintptr(x+40), uintptr(y+30)); ok == 0 {
		release()
		return nil, fmtLastError("SetCursorPos")
	}
	return release, nil
}

// fmtLastError names a failed call.
func fmtLastError(call string) error {
	return fmt.Errorf("e2e: %s failed", call)
}
