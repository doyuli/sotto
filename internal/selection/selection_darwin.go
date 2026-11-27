//go:build darwin

package selection

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/hotkey"
	"sotto/internal/macnative"
)

var (
	axApplication          func(int32) uintptr
	axAttribute            func(uintptr, objc.ID, *uintptr) int32
	axParameter            func(uintptr, objc.ID, uintptr, *uintptr) int32
	axTimeout              func(uintptr, float32) int32
	axValue                func(uintptr, uint32, unsafe.Pointer) bool
	axValueType            func(uintptr) uint32
	axElementID, axValueID func() uint64
	cfRelease              func(uintptr)
	cfTypeID               func(uintptr) uint64
	cfStringID             func() uint64
)

var loadAX = sync.OnceValue(func() error {
	if _, err := macnative.Framework("AppKit"); err != nil {
		return err
	}
	if err := macnative.Bind("CoreFoundation", macnative.Symbol{Target: &cfRelease, Name: "CFRelease"}, macnative.Symbol{Target: &cfTypeID, Name: "CFGetTypeID"}, macnative.Symbol{Target: &cfStringID, Name: "CFStringGetTypeID"}); err != nil {
		return err
	}
	return macnative.Bind("ApplicationServices",
		macnative.Symbol{Target: &axElementID, Name: "AXUIElementGetTypeID"}, macnative.Symbol{Target: &axValueID, Name: "AXValueGetTypeID"},
		macnative.Symbol{Target: &axApplication, Name: "AXUIElementCreateApplication"},
		macnative.Symbol{Target: &axAttribute, Name: "AXUIElementCopyAttributeValue"},
		macnative.Symbol{Target: &axParameter, Name: "AXUIElementCopyParameterizedAttributeValue"},
		macnative.Symbol{Target: &axTimeout, Name: "AXUIElementSetMessagingTimeout"},
		macnative.Symbol{Target: &axValue, Name: "AXValueGetValue"},
		macnative.Symbol{Target: &axValueType, Name: "AXValueGetType"})
})

type macSource struct {
	pid        int32
	generation uint64
	self       bool
}

func prepareMacSource() (s macSource) {
	s.pid, s.generation = macnative.FrontmostPID(), macnative.WorkspaceGeneration()
	s.self = s.pid == int32(os.Getpid())
	// A nonactivating panel may be key while the external app is frontmost.
	// Prepare runs on the UI thread before hiding our panels.
	mygo.RunOnMain(func() {
		macnative.Pool(func() {
			app := macnative.Class("NSApplication").Send(macnative.Sel("sharedApplication"))
			s.self = s.self || app.Send(macnative.Sel("keyWindow")) != 0
		})
	})
	return s
}

func (s macSource) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.self {
		return ErrSelfWindow
	}
	if s.pid <= 0 || s.generation != macnative.WorkspaceGeneration() || s.pid != macnative.FrontmostPID() {
		return ErrSourceChanged
	}
	return nil
}

func Prepare(shortcut string) func(context.Context) (Selection, error) {
	s := prepareMacSource()
	mainKey := hotkey.MacMainKeyCode(shortcut)
	return func(ctx context.Context) (Selection, error) {
		return readMacSelection(ctx, s.check, macnative.RequireAccessibility, func(ctx context.Context) (Selection, error) { return readAX(ctx, s) }, func(ctx context.Context) (Selection, error) { return copyMacSelection(ctx, s, mainKey) })
	}
}

func readMacSelection(ctx context.Context, check func(context.Context) error, permission func(bool) error, read, copy func(context.Context) (Selection, error)) (Selection, error) {
	if err := check(ctx); err != nil {
		return Selection{}, err
	}
	if err := permission(true); err != nil {
		return Selection{}, err
	}
	sel, err := read(ctx)
	if err == nil && sel.Text != "" {
		if err := check(ctx); err != nil {
			return Selection{}, err
		}
		return sel, nil
	}
	if err != nil && !errors.Is(err, ErrNoSelection) {
		return Selection{}, err
	}
	if err := check(ctx); err != nil {
		return Selection{}, err
	}
	return copy(ctx)
}

func grab(ctx context.Context) (Selection, error) { return Prepare("")(ctx) }
func grabUIA(ctx context.Context) (Selection, error) {
	s := prepareMacSource()
	if err := macnative.RequireAccessibility(true); err != nil {
		return Selection{}, err
	}
	return readAX(ctx, s)
}
func grabClipboard(ctx context.Context) (Selection, error) {
	s := prepareMacSource()
	if err := macnative.RequireAccessibility(true); err != nil {
		return Selection{}, err
	}
	return copyMacSelection(ctx, s, -1)
}

var axAdmission = make(chan struct{}, 1)

func readAX(ctx context.Context, s macSource) (sel Selection, err error) {
	if err := loadAX(); err != nil {
		return sel, err
	}
	if err := s.check(ctx); err != nil {
		return sel, err
	}
	select {
	case axAdmission <- struct{}{}:
	case <-ctx.Done():
		return sel, ctx.Err()
	}
	defer func() { <-axAdmission }()
	if err := s.check(ctx); err != nil {
		return sel, err
	}
	macnative.Pool(func() {
		app := axApplication(s.pid)
		if app == 0 {
			err = ErrNoSelection
			return
		}
		defer cfRelease(app)
		timeout := float32(.25)
		if deadline, ok := ctx.Deadline(); ok {
			timeout = min(timeout, float32(time.Until(deadline).Seconds()))
		}
		if timeout <= 0 {
			err = context.DeadlineExceeded
			return
		}
		if code := axTimeout(app, timeout); code != 0 {
			err = fmt.Errorf("AX messaging timeout: %d", code)
			return
		}
		var focus uintptr
		if axAttribute(app, macnative.String("AXFocusedUIElement"), &focus) != 0 || focus == 0 {
			err = ErrNoSelection
			return
		}
		defer cfRelease(focus)
		if cfTypeID(focus) != axElementID() {
			err = ErrNoSelection
			return
		}
		if err = ctx.Err(); err != nil {
			return
		}
		var subrole uintptr
		if axAttribute(focus, macnative.String("AXSubrole"), &subrole) == 0 && subrole != 0 {
			defer cfRelease(subrole)
			if cfTypeID(subrole) == cfStringID() && macnative.GoString(objc.ID(subrole)) == "AXSecureTextField" {
				err = errors.New("无法读取安全输入框中的内容")
				return
			}
		}
		if err = ctx.Err(); err != nil {
			return
		}
		var text uintptr
		if axAttribute(focus, macnative.String("AXSelectedText"), &text) != 0 || text == 0 {
			err = ErrNoSelection
			return
		}
		defer cfRelease(text)
		if cfTypeID(text) != cfStringID() {
			err = ErrNoSelection
			return
		}
		sel = Selection{Text: macnative.GoString(objc.ID(text)), Via: "ax"}
		if err = ctx.Err(); err != nil {
			return
		}
		if sel.Text == "" {
			err = ErrNoSelection
			return
		}
		var span uintptr
		if axAttribute(focus, macnative.String("AXSelectedTextRange"), &span) == 0 && span != 0 {
			defer cfRelease(span)
			if cfTypeID(span) != axValueID() || axValueType(span) != 4 {
				return
			}
			var bounds uintptr
			if axParameter(focus, macnative.String("AXBoundsForRange"), span, &bounds) == 0 && bounds != 0 {
				defer cfRelease(bounds)
				var r macnative.Rect
				if cfTypeID(bounds) == axValueID() && axValueType(bounds) == 3 && axValue(bounds, 3, unsafe.Pointer(&r)) && r.Size.Width > 0 && r.Size.Height > 0 && math.Abs(r.Origin.X) <= 1<<30 && math.Abs(r.Origin.Y) <= 1<<30 && r.Size.Width <= 1<<30 && r.Size.Height <= 1<<30 {
					sel.Rect = Rect{int(math.Round(r.Origin.X)), int(math.Round(r.Origin.Y)), int(math.Round(r.Size.Width)), int(math.Round(r.Size.Height))}
					sel.HasRect = true
				}
			}
		}
	})
	if sourceErr := s.check(ctx); sourceErr != nil {
		return Selection{}, sourceErr
	}
	return sel, err
}

const macControlModifier = 1 << 18

// CapturePrimaryPress is sampled on the UI thread before deferring a blur.
func CapturePrimaryPress() PrimaryPress {
	events := macnative.Class("NSEvent")
	if events.Send(macnative.Sel("pressedMouseButtons"))&1 != 0 &&
		events.Send(macnative.Sel("modifierFlags"))&macControlModifier == 0 {
		return PrimaryPress{At: time.Now(), Point: mygo.Screen.CursorScreenPoint()}
	}
	return PrimaryPress{}
}

func WatchWindowLeave(handle uintptr, changed func(PrimaryPress)) (stop func(), err error) {
	var global, local, activation func()
	mygo.RunOnMain(func() {
		const leftMouseDown = 1
		const mask uint64 = 1<<leftMouseDown | 1<<3 | 1<<25
		notify := func(event objc.ID) {
			var press PrimaryPress
			if event.Send(macnative.Sel("type")) == leftMouseDown &&
				event.Send(macnative.Sel("modifierFlags"))&macControlModifier == 0 {
				press = PrimaryPress{At: time.Now(), Point: mygo.Screen.CursorScreenPoint()}
			}
			go changed(press)
		}
		global, err = macnative.Monitor(mask, false, notify)
		if err != nil {
			return
		}
		local, err = macnative.Monitor(mask, true, func(event objc.ID) {
			hit := event.Send(macnative.Sel("window"))
			if hit != 0 && !macnative.MenuWindow(hit) && !macnative.WindowContains(objc.ID(handle), hit) {
				notify(event)
			}
		})
		if err != nil {
			global()
			return
		}
		activation, err = macnative.WatchActivation(func() { go changed(PrimaryPress{}) })
		if err != nil {
			global()
			local()
		}
	})
	if err != nil {
		return nil, err
	}
	return sync.OnceFunc(func() { mygo.RunOnMain(func() { global(); local(); activation() }) }), nil
}

func WindowOwnsForeground(handle uintptr) bool {
	var owns bool
	mygo.RunOnMain(func() {
		app := macnative.Class("NSApplication").Send(macnative.Sel("sharedApplication"))
		key := app.Send(macnative.Sel("keyWindow"))
		owns = macnative.WindowContains(objc.ID(handle), key) || macnative.MenuWindow(key)
	})
	return owns
}
