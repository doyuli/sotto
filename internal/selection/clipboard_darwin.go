//go:build darwin

package selection

import (
	"context"
	"errors"
	"fmt"
	"log"
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

func savePasteboard(ctx context.Context) ([]pasteboardItem, error) {
	return snapshotMacPasteboard(ctx, pasteboard, pasteboardOnMain)
}

// Yield the main thread between representations and copy immutable NSData on
// the caller's background goroutine. A promised dataForType call is still
// synchronous; cancellation is checked before and after each AppKit turn.
func snapshotMacPasteboard(ctx context.Context, getBoard func() objc.ID, onMain func(func())) ([]pasteboardItem, error) {
	var board, array objc.ID
	var items []objc.ID
	var types [][]string
	var change int64
	err := ErrSourceChanged // RunOnMain may drop work during shutdown.
	onMain(func() {
		if err = ctx.Err(); err != nil {
			return
		}
		board = getBoard().Send(macnative.Sel("retain"))
		change = objc.Send[int64](board, macnative.Sel("changeCount"))
		array = board.Send(macnative.Sel("pasteboardItems")).Send(macnative.Sel("retain"))
		if objc.Send[uint64](array, macnative.Sel("count")) > 1024 {
			err = ErrClipboardUnsafe
			return
		}
		items = macnative.Items(array)
		if len(items) == 0 && objc.Send[uint64](board.Send(macnative.Sel("types")), macnative.Sel("count")) != 0 {
			err = ErrClipboardUnsafe
			return
		}
		types = make([][]string, len(items))
		for i, item := range items {
			names := item.Send(macnative.Sel("types"))
			if objc.Send[uint64](names, macnative.Sel("count")) > 256 {
				err = ErrClipboardUnsafe
				return
			}
			for _, name := range macnative.Items(names) {
				types[i] = append(types[i], macnative.GoString(name))
			}
		}
	})
	defer onMain(func() {
		array.Send(macnative.Sel("release"))
		board.Send(macnative.Sel("release"))
	})
	if err != nil {
		return nil, err
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if objc.Send[int64](board, macnative.Sel("changeCount")) != change {
			return ErrSourceChanged
		}
		return nil
	}
	data, err := snapshotPasteboard(types, func(i int, name string, budget int) ([]byte, error) {
		var value objc.ID
		readErr := ErrSourceChanged
		onMain(func() {
			if readErr = check(); readErr != nil {
				return
			}
			value = items[i].Send(macnative.Sel("dataForType:"), macnative.String(name))
			if value == 0 || objc.Send[uint64](value, macnative.Sel("length")) > uint64(max(budget, 0)) {
				value, readErr = 0, ErrClipboardUnsafe
				return
			}
			// copy returns an immutable object whose lifetime spans this pool.
			value = value.Send(macnative.Sel("copy"))
			readErr = check()
		})
		defer value.Send(macnative.Sel("release"))
		if readErr != nil {
			return nil, readErr
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return macnative.Bytes(value, budget)
	})
	if err != nil {
		return nil, err
	}
	err = ErrSourceChanged
	onMain(func() { err = check() })
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Materialize the native replacement before clearing and recheck the version
// after the clear, since macOS has no pasteboard lock.
func restorePasteboard(board objc.ID, saved []pasteboardItem, expected int64, guard func() bool) error {
	var objects objc.ID
	return restorePasteboardContents(expected, pasteboardRestoreOps{
		prepare: func() error {
			objects = macnative.Array()
			for _, formats := range saved {
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
		allowed: guard,
		change:  func() int64 { return objc.Send[int64](board, macnative.Sel("changeCount")) },
		clear:   func() int64 { return objc.Send[int64](board, macnative.Sel("clearContents")) },
		write: func() error {
			if len(saved) != 0 && !objc.Send[bool](board, macnative.Sel("writeObjects:"), objects) {
				return fmt.Errorf("restore pasteboard items")
			}
			return nil
		},
	})
}

func copyControlsHeld() bool {
	// A shortcut's main key can come up after its modifiers. Its release
	// would otherwise be mistaken for intervening input by the copy monitor.
	// Caps Lock reports a latched state and does not modify the copy chord.
	for key := uint16(0); key < 128; key++ {
		if key != 57 && keyState(1, key) {
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

// Monitor key, modifier, and mouse button events. Scrolling can continue during capture.
const macCopyInputMask uint64 = 1<<1 | 1<<2 | 1<<3 | 1<<4 | 1<<10 | 1<<11 | 1<<12 | 1<<25 | 1<<26

func watchMacCopyInput() (*copyInputState, func(), error) {
	state := &copyInputState{marker: uintptr(0x5354544f00000000 | copyMarker.Add(1))}
	var stops []func()
	var err error
	mygo.RunOnMain(func() {
		for _, local := range []bool{false, true} {
			stop, e := macnative.Monitor(macCopyInputMask, local, func(event objc.ID) {
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
	if err := waitCopyRelease(ctx, copyControlsHeld); err != nil {
		return Selection{}, err
	}
	watch, stop, err := watchMacCopyInput()
	if err != nil {
		return Selection{}, fmt.Errorf("%w: %w", ErrClipboardUnsafe, err)
	}
	defer stop()
	before := pasteboardChange()
	saved, err := savePasteboard(ctx)
	if err != nil {
		if cancelErr := ctx.Err(); cancelErr != nil {
			return Selection{}, cancelErr
		}
		if errors.Is(err, ErrSourceChanged) {
			return Selection{}, err
		}
		log.Printf("selection: clipboard copy without restore: %v", err)
	}

	if err := source.check(ctx); err != nil {
		return Selection{}, err
	}
	if pasteboardChange() != before || copyControlsHeld() || !watch.unchanged() || secureInput() {
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
	for _, event := range []uintptr{down, up} {
		setFlags(event, 1<<20)
		setField(event, 42, int64(watch.marker))
		postEvent(1, event)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	wait := time.NewTimer(time.Second)
	defer wait.Stop()
	for {
		select {
		case <-ctx.Done():
			return Selection{}, ctx.Err()
		case <-wait.C:
			return Selection{}, context.DeadlineExceeded
		case <-tick.C:
			if err := source.check(ctx); err != nil {
				return Selection{}, err
			}
			if !watch.unchanged() || secureInput() {
				return Selection{}, ErrSourceChanged
			}
			n := pasteboardChange()
			if n == before {
				continue
			}
			var text string
			pasteboardOnMain(func() {
				text = macnative.GoString(pasteboard().Send(macnative.Sel("stringForType:"), macnative.String("public.utf8-plain-text")))
			})
			if err := source.check(ctx); err != nil {
				return Selection{}, err
			}
			if !watch.unchanged() || pasteboardChange() != n || secureInput() {
				return Selection{}, ErrSourceChanged
			}
			if saved == nil {
				return finishClipboardCopy(ctx, text, nil)
			}
			err := ErrSourceChanged // Main-thread dispatch may stop during shutdown.
			pasteboardOnMain(func() {
				err = restorePasteboard(pasteboard(), saved, n, func() bool {
					return watch.unchanged() && source.check(ctx) == nil && !secureInput()
				})
			})
			if err != nil && (!watch.unchanged() || secureInput()) {
				err = ErrSourceChanged
			}
			if sourceErr := source.check(ctx); sourceErr != nil {
				return Selection{}, sourceErr
			}
			return finishClipboardCopy(ctx, text, err)
		}
	}
}
