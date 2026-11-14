//go:build darwin

package hotkey

import (
	"fmt"
	"log"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
	"sotto/internal/macnative"
)

// CarbonEvents.h documents that non-exclusive registrations can succeed but
// receive no events. Sotto owns exclusive registrations instead, without
// changing MyGo or requiring an event tap / Input Monitoring permission.
const hotkeySignature uint32 = 0x5374746f // 'Stto'
type hotkeyID struct{ Signature, ID uint32 }
type eventSpec struct{ Class, Kind uint32 }

var carbon struct {
	register   func(uint32, uint32, hotkeyID, uintptr, uint32, *uintptr) int32
	unregister func(uintptr) int32
	target     func() uintptr
	install    func(uintptr, uintptr, uint32, *eventSpec, uintptr, *uintptr) int32
	remove     func(uintptr) int32
	parameter  func(uintptr, uint32, uint32, uintptr, uintptr, *uintptr, unsafe.Pointer) int32
	symbolic   func(*objc.ID) int32
}
var loadCarbon = sync.OnceValue(func() error {
	if _, err := macnative.Framework("Foundation"); err != nil {
		return err
	}
	return macnative.Bind("Carbon",
		macnative.Symbol{Target: &carbon.register, Name: "RegisterEventHotKey"},
		macnative.Symbol{Target: &carbon.unregister, Name: "UnregisterEventHotKey"},
		macnative.Symbol{Target: &carbon.target, Name: "GetApplicationEventTarget"},
		macnative.Symbol{Target: &carbon.install, Name: "InstallEventHandler"},
		macnative.Symbol{Target: &carbon.remove, Name: "RemoveEventHandler"},
		macnative.Symbol{Target: &carbon.parameter, Name: "GetEventParameter"},
		macnative.Symbol{Target: &carbon.symbolic, Name: "CopySymbolicHotKeys"},
	)
})

type nativeBinding struct {
	ref uintptr
	id  uint32
	fn  func()
}
type systemBackend struct {
	// All fields belong to the main thread, including Carbon object lifetimes.
	bindings          map[string]nativeBinding
	next              uint32
	handler, callback uintptr
	closed            bool
}

func NewBackend() Backend { return &systemBackend{bindings: make(map[string]nativeBinding)} }

func (b *systemBackend) Register(value string, fn func()) error {
	s, err := Parse(value, "darwin")
	if err != nil {
		return err
	}
	code, ok := macKeyCodes[s.Key]
	if !ok {
		return fmt.Errorf("macOS 暂不支持按键 %s", s.Key)
	}
	if err := loadCarbon(); err != nil {
		return fmt.Errorf("加载快捷键组件：%w", err)
	}
	err = fmt.Errorf("应用已退出，无法注册快捷键")
	mygo.RunOnMain(func() {
		if b.closed {
			return
		}
		macnative.Pool(func() { err = b.register(s, code, fn) })
	})
	return err
}

func (b *systemBackend) register(s Shortcut, code uint32, fn func()) error {
	value := s.String()
	if _, exists := b.bindings[value]; exists {
		return fmt.Errorf("该组合键已用于 Sotto 的其他操作")
	}
	mods := macModifiers(s.Mods)
	if err := checkSystemShortcut(code, mods); err != nil {
		return err
	}
	if b.handler == 0 {
		if b.callback == 0 {
			b.callback = purego.NewCallback(func(next, event, data uintptr) int32 {
				var id hotkeyID
				if carbon.parameter(event, 0x2d2d2d2d, 0x686b6964, 0, unsafe.Sizeof(id), nil, unsafe.Pointer(&id)) != 0 || id.Signature != hotkeySignature {
					return -9874
				}
				for _, binding := range b.bindings {
					if binding.id == id.ID {
						binding.fn()
						return 0
					}
				}
				return -9874
			})
		}
		spec := eventSpec{Class: 0x6b657962, Kind: 5}
		if status := carbon.install(carbon.target(), b.callback, 1, &spec, 0, &b.handler); status != 0 {
			return fmt.Errorf("无法监听快捷键（系统错误 %d）", status)
		}
	}
	b.next++
	var ref uintptr
	// kEventHotKeyExclusive = 1. An existing holder must cause a visible failure.
	if status := carbon.register(code, mods, hotkeyID{hotkeySignature, b.next}, carbon.target(), 1, &ref); status != 0 {
		if status == -9878 {
			return fmt.Errorf("该组合键已被其他应用占用，请更换组合键")
		}
		return fmt.Errorf("系统无法注册该组合键（错误 %d），请更换组合键", status)
	}
	b.bindings[value] = nativeBinding{ref, b.next, fn}
	return nil
}

func (b *systemBackend) Unregister(value string) {
	mygo.RunOnMain(func() {
		value = Canonical(value, "darwin")
		if binding, ok := b.bindings[value]; ok {
			if status := carbon.unregister(binding.ref); status != 0 {
				log.Printf("unregister hotkey %s: OSStatus %d", value, status)
			}
			delete(b.bindings, value)
		}
	})
}

func (b *systemBackend) Close() {
	mygo.RunOnMain(func() {
		b.closed = true
		for value := range b.bindings {
			b.Unregister(value)
		}
		if b.handler != 0 {
			carbon.remove(b.handler)
			b.handler = 0
		}
	})
}

func macModifiers(m Modifiers) uint32 {
	var out uint32
	if m&Command != 0 {
		out |= 1 << 8
	}
	if m&Shift != 0 {
		out |= 1 << 9
	}
	if m&Option != 0 {
		out |= 1 << 11
	}
	if m&Control != 0 {
		out |= 1 << 12
	}
	return out
}

// CopySymbolicHotKeys excludes application menu equivalents. Do not claim
// that registration proves the absence of every other application's shortcut.
func checkSystemShortcut(code, mods uint32) error {
	var array objc.ID
	if status := carbon.symbolic(&array); status != 0 {
		return fmt.Errorf("无法检查系统快捷键（错误 %d），请重试", status)
	}
	if array == 0 {
		return fmt.Errorf("无法读取系统快捷键，请重试")
	}
	defer array.Send(macnative.Sel("release"))
	codeKey := macnative.String("kHISymbolicHotKeyCode")
	modsKey := macnative.String("kHISymbolicHotKeyModifiers")
	enabledKey := macnative.String("kHISymbolicHotKeyEnabled")
	for _, item := range macnative.Items(array) {
		key := item.Send(macnative.Sel("objectForKey:"), codeKey)
		modifiers := item.Send(macnative.Sel("objectForKey:"), modsKey)
		enabled := item.Send(macnative.Sel("objectForKey:"), enabledKey)
		if key != 0 && modifiers != 0 && enabled != 0 && objc.Send[bool](enabled, macnative.Sel("boolValue")) && objc.Send[uint32](key, macnative.Sel("unsignedIntValue")) == code && objc.Send[uint32](modifiers, macnative.Sel("unsignedIntValue")) == mods {
			return fmt.Errorf("该组合键已用于 macOS 系统功能，请更换组合键或在系统设置中调整")
		}
	}
	return nil
}

// ANSI virtual positions from Events.h, matching PhysicalKey in MyGo's Mac input.
var macKeyCodes = map[string]uint32{
	"A": 0x00, "S": 0x01, "D": 0x02, "F": 0x03, "H": 0x04, "G": 0x05, "Z": 0x06, "X": 0x07,
	"C": 0x08, "V": 0x09, "B": 0x0b, "Q": 0x0c, "W": 0x0d, "E": 0x0e, "R": 0x0f, "Y": 0x10,
	"T": 0x11, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15, "6": 0x16, "5": 0x17, "=": 0x18,
	"9": 0x19, "7": 0x1a, "-": 0x1b, "8": 0x1c, "0": 0x1d, "]": 0x1e, "O": 0x1f, "U": 0x20,
	"[": 0x21, "I": 0x22, "P": 0x23, "L": 0x25, "J": 0x26, "'": 0x27, "K": 0x28, ";": 0x29,
	"\\": 0x2a, ",": 0x2b, "/": 0x2c, "N": 0x2d, "M": 0x2e, ".": 0x2f, "`": 0x32,
	"Enter": 0x24, "Tab": 0x30, "Space": 0x31, "Backspace": 0x33, "Escape": 0x35,
	"Delete": 0x75, "Home": 0x73, "End": 0x77, "PageUp": 0x74, "PageDown": 0x79, "Insert": 0x72,
	"Left": 0x7b, "Right": 0x7c, "Down": 0x7d, "Up": 0x7e,
	"F1": 0x7a, "F2": 0x78, "F3": 0x63, "F4": 0x76, "F5": 0x60, "F6": 0x61, "F7": 0x62, "F8": 0x64,
	"F9": 0x65, "F10": 0x6d, "F11": 0x67, "F12": 0x6f, "F13": 0x69, "F14": 0x6b, "F15": 0x71,
	"F16": 0x6a, "F17": 0x40, "F18": 0x4f, "F19": 0x50, "F20": 0x5a,
	"VolumeUp": 0x48, "VolumeDown": 0x49, "VolumeMute": 0x4a,
	"Num0": 0x52, "Num1": 0x53, "Num2": 0x54, "Num3": 0x55, "Num4": 0x56, "Num5": 0x57,
	"Num6": 0x58, "Num7": 0x59, "Num8": 0x5b, "Num9": 0x5c, "NumDec": 0x41, "NumMult": 0x43,
	"NumAdd": 0x45, "NumDiv": 0x4b, "NumSub": 0x4e,
}
