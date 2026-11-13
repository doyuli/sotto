//go:build darwin

package selection

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

var (
	keyState    func(int32, uint16) bool
	buttonState func(int32, uint32) bool
	createKey   func(uintptr, uint16, bool) uintptr
	setFlags    func(uintptr, uint64)
	setField    func(uintptr, uint32, int64)
	getField    func(uintptr, uint32) int64
	postEvent   func(uint32, uintptr)
	copyMarker  atomic.Uint64
	secureInput func() bool
)

var loadCopy = sync.OnceValue(func() error {
	if err := macnative.Bind("Carbon", macnative.Symbol{Target: &secureInput, Name: "IsSecureEventInputEnabled"}); err != nil {
		return err
	}
	return macnative.Bind("CoreGraphics",
		macnative.Symbol{Target: &keyState, Name: "CGEventSourceKeyState"}, macnative.Symbol{Target: &buttonState, Name: "CGEventSourceButtonState"},
		macnative.Symbol{Target: &createKey, Name: "CGEventCreateKeyboardEvent"}, macnative.Symbol{Target: &setFlags, Name: "CGEventSetFlags"},
		macnative.Symbol{Target: &setField, Name: "CGEventSetIntegerValueField"}, macnative.Symbol{Target: &getField, Name: "CGEventGetIntegerValueField"},
		macnative.Symbol{Target: &postEvent, Name: "CGEventPost"})
})

func pasteboard() objc.ID {
	return macnative.Class("NSPasteboard").Send(macnative.Sel("generalPasteboard"))
}

// Share MyGo's clipboard thread so its provider callbacks and result-copy
// operations cannot concurrently mutate the same AppKit pasteboard proxy.
func pasteboardOnMain(fn func()) {
	mygo.RunOnMain(func() { macnative.Pool(fn) })
}

func pasteboardChange() (n int64) {
	pasteboardOnMain(func() { n = objc.Send[int64](pasteboard(), macnative.Sel("changeCount")) })
	return
}

func savePasteboard() (saved *clipboardSnapshot, err error) {
	pasteboardOnMain(func() {
		board := pasteboard()
		n := objc.Send[int64](board, macnative.Sel("changeCount"))
		array := board.Send(macnative.Sel("pasteboardItems"))
		if objc.Send[uint64](array, macnative.Sel("count")) > 1024 {
			err = ErrClipboardUnsafe
			return
		}
		items := macnative.Items(array)
		if len(items) == 0 && objc.Send[uint64](board.Send(macnative.Sel("types")), macnative.Sel("count")) != 0 {
			err = ErrClipboardUnsafe
			return
		}
		types := make([][]string, len(items))
		for i, item := range items {
			for _, name := range macnative.Items(item.Send(macnative.Sel("types"))) {
				types[i] = append(types[i], macnative.GoString(name))
			}
		}
		data, readErr := snapshotPasteboard(types, func(i int, name string, budget int) ([]byte, error) {
			return macnative.Bytes(items[i].Send(macnative.Sel("dataForType:"), macnative.String(name)), budget)
		})
		if readErr != nil {
			err = readErr
			return
		}
		if objc.Send[int64](board, macnative.Sel("changeCount")) != n {
			err = ErrSourceChanged
			return
		}
		saved = &clipboardSnapshot{mac: &macClipboardSnapshot{items: data, change: n}}
	})
	return
}

// Materialize the native replacement before clearing anything. There is no
// pasteboard lock/CAS on macOS: recheck after clear too, and retain failures.
func restorePasteboard(saved *clipboardSnapshot, expected int64, guard func() bool) (restored bool, retryChange int64, err error) {
	// RunOnMain drops work after shutdown; that must not create a retry target.
	retryChange, err = -1, ErrSourceChanged
	pasteboardOnMain(func() {
		board := pasteboard()
		var objects objc.ID
		restored, retryChange, err = restorePasteboardContents(expected, pasteboardRestoreOps{
			prepare: func() error {
				objects = macnative.Array()
				for _, formats := range saved.mac.items {
					item := macnative.Class("NSPasteboardItem").Send(macnative.Sel("alloc")).Send(macnative.Sel("init"))
					item.Send(macnative.Sel("autorelease"))
					for _, f := range formats {
						if !objc.Send[bool](item, macnative.Sel("setData:forType:"), macnative.Data(f.data), macnative.String(f.name)) {
							return fmt.Errorf("prepare pasteboard type %s", f.name)
						}
					}
					objects.Send(macnative.Sel("addObject:"), item)
				}
				return nil
			},
			allowed: func() bool { return guard == nil || guard() },
			change:  func() int64 { return objc.Send[int64](board, macnative.Sel("changeCount")) },
			clear:   func() int64 { return objc.Send[int64](board, macnative.Sel("clearContents")) },
			write: func() error {
				if len(saved.mac.items) != 0 && !objc.Send[bool](board, macnative.Sel("writeObjects:"), objects) {
					return fmt.Errorf("restore pasteboard items")
				}
				return nil
			},
		})
	})
	return
}

func retainPasteboard(saved *clipboardSnapshot, expected int64) {
	// -1 means that a concurrent input/source change made ownership unknown.
	// Keep the backup, but require discarding it rather than overwriting newer data.
	saved.mac.restoreChange = expected
	retainClipboardRecovery(saved, 0)
}

func RecoverClipboard(id string) error {
	<-clipboardMu
	defer func() { clipboardMu <- struct{}{} }()
	clipboardRecovery.Lock()
	saved, activeID := clipboardRecovery.snapshot, clipboardRecovery.id
	clipboardRecovery.Unlock()
	if id == "" || id != activeID || saved == nil || saved.mac == nil {
		return ErrClipboardRecoveryGone
	}
	ok, retryChange, err := restorePasteboard(saved, saved.mac.restoreChange, nil)
	if !ok {
		saved.mac.restoreChange = retryChange
		return err
	}
	clipboardRecovery.Lock()
	clipboardRecovery.id, clipboardRecovery.snapshot = "", nil
	clipboardRecovery.Unlock()
	return nil
}

func copyControlsHeld() bool {
	// ANSI modifiers and C; also refuse to copy during any pointer gesture.
	for _, key := range []uint16{54, 55, 56, 60, 58, 61, 59, 62, 8} {
		if keyState(1, key) {
			return true
		}
	}
	for b := uint32(0); b < 5; b++ {
		if buttonState(1, b) {
			return true
		}
	}
	return false
}

func watchMacCopyInput() (*copyInputState, func(), error) {
	state := &copyInputState{marker: uintptr(0x5354544f00000000 | copyMarker.Add(1))}
	var stops []func()
	var err error
	mygo.RunOnMain(func() {
		const mask uint64 = 1<<1 | 1<<2 | 1<<3 | 1<<4 | 1<<10 | 1<<11 | 1<<12 | 1<<22 | 1<<25 | 1<<26
		for _, local := range []bool{false, true} {
			stop, e := macnative.Monitor(mask, local, func(event objc.ID) {
				cg := event.Send(macnative.Sel("CGEvent"))
				if cg == 0 || uintptr(getField(uintptr(cg), 42)) != state.marker {
					state.invalidate(copyInputKey)
				}
			})
			if e != nil {
				err = e
				break
			}
			stops = append(stops, stop)
		}
	})
	stop := sync.OnceFunc(func() {
		mygo.RunOnMain(func() {
			for _, fn := range stops {
				fn()
			}
		})
	})
	if err != nil {
		stop()
		return nil, nil, err
	}
	return state, stop, nil
}

func copyMacSelection(ctx context.Context, source macSource) (Selection, error) {
	if err := loadAX(); err != nil {
		return Selection{}, err
	}
	if err := loadCopy(); err != nil {
		return Selection{}, err
	}
	if err := source.check(ctx); err != nil {
		return Selection{}, err
	}
	if secureInput() {
		return Selection{}, ErrClipboardUnsafe
	}
	select {
	case <-ctx.Done():
		return Selection{}, ctx.Err()
	case <-clipboardMu:
	}
	defer func() { clipboardMu <- struct{}{} }()
	if PendingClipboardRecovery() != "" {
		return Selection{}, ErrClipboardRestore
	}
	deadline := time.NewTimer(400 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for copyControlsHeld() {
		select {
		case <-ctx.Done():
			return Selection{}, ctx.Err()
		case <-deadline.C:
			return Selection{}, ErrShortcutHeld
		case <-tick.C:
		}
	}
	saved, err := savePasteboard()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	watch, stop, err := watchMacCopyInput()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer stop()
	if err := source.check(ctx); err != nil {
		return Selection{}, err
	}
	if pasteboardChange() != saved.mac.change || copyControlsHeld() || !watch.unchanged() || secureInput() {
		return Selection{}, ErrSourceChanged
	}
	down, up := createKey(0, 8, true), createKey(0, 8, false)
	if down == 0 || up == 0 {
		if down != 0 {
			cfRelease(down)
		}
		if up != 0 {
			cfRelease(up)
		}
		return Selection{}, fmt.Errorf("create copy key events")
	}
	defer cfRelease(down)
	defer cfRelease(up)
	restored := false
	defer func() {
		// Cancellation or timeout can win the select after the source already
		// copied. Preserve the original even on those early return paths.
		if !restored && PendingClipboardRecovery() == "" && pasteboardChange() != saved.mac.change {
			retainPasteboard(saved, -1)
		}
	}()
	for _, event := range []uintptr{down, up} {
		setFlags(event, 1<<20)
		setField(event, 42, int64(watch.marker))
		postEvent(1, event)
	}
	wait := time.NewTimer(time.Second)
	defer wait.Stop()
	for {
		select {
		case <-ctx.Done():
			return Selection{}, ctx.Err()
		case <-wait.C:
			return Selection{}, ErrNoSelection
		case <-tick.C:
			n := pasteboardChange()
			if n == saved.mac.change {
				if err := source.check(ctx); err != nil {
					return Selection{}, err
				}
				continue
			}
			// Once a copy wrote, every failure preserves the original snapshot.
			if !watch.unchanged() || source.check(ctx) != nil || secureInput() {
				retainPasteboard(saved, -1)
				return Selection{}, ErrClipboardRestore
			}
			var text string
			pasteboardOnMain(func() {
				text = macnative.GoString(pasteboard().Send(macnative.Sel("stringForType:"), macnative.String("public.utf8-plain-text")))
			})
			if !watch.unchanged() || source.check(ctx) != nil || pasteboardChange() != n || secureInput() {
				retainPasteboard(saved, -1)
				return Selection{}, ErrClipboardRestore
			}
			if ok, expected, err := restorePasteboard(saved, n, func() bool { return watch.unchanged() && source.check(ctx) == nil && !secureInput() }); !ok {
				if !watch.unchanged() || source.check(ctx) != nil {
					expected = -1
				}
				retainPasteboard(saved, expected)
				return Selection{}, fmt.Errorf("%w: %w", ErrClipboardRestore, err)
			}
			restored = true
			if text == "" {
				return Selection{}, ErrNoSelection
			}
			return Selection{Text: text, Via: "clipboard"}, nil
		}
	}
}
