//go:build e2e && windows

package e2e

import (
	"fmt"
	"strings"
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
	procIsWindowVisible            = modUser32.NewProc("IsWindowVisible")
	procGetWindowLongW             = modUser32.NewProc("GetWindowLongW")
	procGetLayeredWindowAttributes = modUser32.NewProc("GetLayeredWindowAttributes")
	procCreateWindowExW            = modUser32.NewProc("CreateWindowExW")
	procDestroyWindow              = modUser32.NewProc("DestroyWindow")
	procDefWindowProcW             = modUser32.NewProc("DefWindowProcW")
	procRegisterClassExW           = modUser32.NewProc("RegisterClassExW")
	procAllowSetForegroundWindow   = modUser32.NewProc("AllowSetForegroundWindow")

	procGetModuleHandleW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
)

const (
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
	vkMenu         = 0x12                // Alt
	gwlExStyle     = uintptr(0xFFFFFFEC) // -20 as an index
	wsExLayered    = 0x00080000
	lwaAlpha       = 2
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

// tap sends Alt+vk down and up, as a user's chord would arrive.
func tap(vk uint16) {
	tapAccel("Alt+" + keyName(vk))
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
	}
	return 0
}

// keyName is a virtual key's accelerator name, the inverse of keyVK.
func keyName(vk uint16) string {
	switch {
	case vk >= 'A' && vk <= 'Z', vk >= '0' && vk <= '9':
		return string(rune(vk))
	case vk >= 0x70 && vk <= 0x7B:
		return fmt.Sprintf("F%d", vk-0x70+1)
	case vk == 0x20:
		return "Space"
	}
	return ""
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

// visibleWindows lists the app's visible top-level windows: a count and
// whether any is layered, with its alpha.
func visibleWindows(pid uint32) (count int, layered bool, alpha byte) {
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			return 1
		}
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		if vis == 0 {
			return 1
		}
		count++
		ex, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
		if ex&wsExLayered != 0 {
			layered = true
			var key uint32
			var a byte
			var flags uint32
			if ok, _, _ := procGetLayeredWindowAttributes.Call(hwnd, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&a)), uintptr(unsafe.Pointer(&flags))); ok != 0 {
				alpha = a
			}
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return count, layered, alpha
}

// fmtLastError names a failed call.
func fmtLastError(call string) error {
	return fmt.Errorf("e2e: %s failed", call)
}
