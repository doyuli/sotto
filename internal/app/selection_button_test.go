package app

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/selection"
)

func TestSelectionButtonRetriesAnEmptySelection(t *testing.T) {
	// A web view can publish its selected text after the first probe.
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		sel, found := probeSelectionButton(t.Context(), func(context.Context) (selection.Selection, error) {
			if time.Since(start) < 300*time.Millisecond {
				return selection.Selection{}, selection.ErrNoSelection
			}
			return selection.Selection{Text: "selected text"}, nil
		})
		if !found || sel.Text != "selected text" {
			t.Fatalf("selection = %+v, found = %t", sel, found)
		}
	})
}

func TestSelectionButtonSlowNativeRead(t *testing.T) {
	// A slow accessibility provider can finish after the old 450 ms deadline.
	for _, tc := range []struct {
		name  string
		delay time.Duration
		want  bool
	}{
		{"within deadline", time.Second, true}, {"late result", 2 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), selectionButtonWait)
				defer cancel()
				_, shown := probeSelectionButton(ctx, func(context.Context) (selection.Selection, error) {
					time.Sleep(tc.delay)
					return selection.Selection{Text: "native selection"}, nil
				})
				if shown != tc.want {
					t.Fatalf("shown = %t, want %t", shown, tc.want)
				}
			})
		})
	}
}

func TestSelectionButtonEmptyNativeRead(t *testing.T) {
	// An empty native selection should leave the icon hidden.
	synctest.Test(t, func(t *testing.T) {
		_, shown := probeSelectionButton(t.Context(), func(context.Context) (selection.Selection, error) {
			return selection.Selection{}, selection.ErrNoSelection
		})
		if shown {
			t.Fatal("empty selection showed the button")
		}
	})
}

func TestSelectionButtonStopsProbingWhenSourceChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		_, found := probeSelectionButton(t.Context(), func(context.Context) (selection.Selection, error) {
			attempts++
			return selection.Selection{}, selection.ErrSourceChanged
		})
		if found || attempts != 1 {
			t.Fatalf("selection found = %t, probes = %d", found, attempts)
		}
	})
}

func TestSelectionButtonUsesTheConfirmedTextWhileItIsCurrent(t *testing.T) {
	// The popup can read a webpage selection that the focused element cannot.
	watch := &selectionButtonWatch{}
	want := selection.Selection{Text: "selected webpage text", Via: "ax"}
	var sourceErr error
	grab := watch.confirmedSelection(1, want, func(context.Context) error { return sourceErr })
	if got, err := grab(t.Context()); err != nil || got != want {
		t.Fatalf("clicked selection = %+v, err = %v", got, err)
	}
	sourceErr = selection.ErrSourceChanged
	if got, err := grab(t.Context()); !errors.Is(err, selection.ErrSourceChanged) || got.Text != "" {
		t.Fatalf("changed source returned %+v, err = %v", got, err)
	}
	sourceErr = nil
	watch.invalidateThrough(2)
	if got, err := grab(t.Context()); !errors.Is(err, selection.ErrSourceChanged) || got.Text != "" {
		t.Fatalf("canceled selection returned %+v, err = %v", got, err)
	}
}

func TestSelectionButtonDropsAnIncompleteGestureOnInputOverflow(t *testing.T) {
	watch := &selectionButtonWatch{}
	button := selectionButton{watch: watch}
	input := make(chan selectionPointerInput, 1)
	down := selection.SelectionPointer{Kind: selection.SelectionPointerDown, At: time.Now()}
	button.gesture.update(down)
	button.grab = watch.confirmedSelection(0, selection.Selection{Text: "old text"}, func(ctx context.Context) error {
		return ctx.Err()
	})

	// Dropping a mouse-up must clear the old selection before another
	// queued edge can complete a gesture with it.
	watch.enqueue(input, down)
	watch.enqueue(input, selection.SelectionPointer{Kind: selection.SelectionPointerUp})
	if button.acceptPointerInput((<-input).version) || !button.gesture.down.At.IsZero() || button.grab != nil {
		t.Fatal("input overflow kept an incomplete gesture or its selection")
	}
	watch.enqueue(input, down)
	if !button.acceptPointerInput((<-input).version) {
		t.Fatal("a fresh gesture was rejected after clearing the overflow")
	}
}

func TestSelectionButtonGesture(t *testing.T) {
	now := time.Unix(1, 0)
	point := mygo.Point{X: 20, Y: 30}
	otherPoint := mygo.Point{X: 60, Y: 30}
	pointer := func(kind selection.SelectionPointerKind, offset time.Duration, point mygo.Point) selection.SelectionPointer {
		return selection.SelectionPointer{Kind: kind, At: now.Add(offset), Point: point}
	}
	down, up := selection.SelectionPointerDown, selection.SelectionPointerUp
	var gesture selectionButtonGesture
	// A plain click can leave an old selection in the source app.
	for _, event := range []selection.SelectionPointer{
		pointer(up, 0, point),
		pointer(down, 10*time.Millisecond, point),
		pointer(up, 20*time.Millisecond, point),
		pointer(down, 40*time.Millisecond, point),
	} {
		if gesture.update(event) {
			t.Fatalf("started a probe before a selection gesture completed: %+v", event)
		}
	}
	if !gesture.update(pointer(up, 50*time.Millisecond, point)) {
		t.Fatal("double-click did not probe for a selected word")
	}
	// The third click replaces the word selection with the whole paragraph.
	gesture.update(pointer(down, 70*time.Millisecond, point))
	if !gesture.update(pointer(up, 80*time.Millisecond, point)) {
		t.Fatal("triple-click did not probe for the final selection")
	}
	gesture.update(pointer(down, time.Second, point))
	if !gesture.update(pointer(up, 2*time.Second, otherPoint)) {
		t.Fatal("drag did not probe for selected text")
	}
}

func TestSelectionButtonClick(t *testing.T) {
	clicks := 0
	tester := ui.NewTester(func(c *ui.Context) {
		renderSelectionButton(c, func() { clicks++ })
	}, selectionButtonSize, selectionButtonSize)
	tester.Frame()
	// A native theme background would bring back the square around the mark.
	frame := tester.Image()
	if frame.RGBAAt(1, 1).A != 0 || frame.RGBAAt(selectionButtonSize/2, selectionButtonSize/2).A == 0 {
		t.Fatal("the mark should be visible on a transparent background")
	}
	if clicks != 0 {
		t.Fatal("showing the button started translation")
	}
	if err := tester.Click("翻译选中文字"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Fatalf("click triggered translation %d times", clicks)
	}
}

func TestSelectionButtonPositionFitsDisplay(t *testing.T) {
	work := mygo.Rectangle{X: -500, Y: 30, Width: 500, Height: 400}
	point := mygo.Point{X: -2, Y: 428}
	got := selectionButtonPosition(point, work)
	if got != (mygo.Point{X: -40, Y: 390}) {
		t.Fatalf("button position = %+v, want it flipped away from both display edges", got)
	}
}
