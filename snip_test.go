package main

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestSnipSelectionGeometry(t *testing.T) {
	for _, tt := range []struct {
		name       string
		start, end snipPoint
		want       *snipRect
	}{
		{"forward", snipPoint{10, 20}, snipPoint{110, 70}, &snipRect{10, 20, 100, 50}},
		{"reverse", snipPoint{110, 70}, snipPoint{10, 20}, &snipRect{10, 20, 100, 50}},
		{"clamped", snipPoint{-10, -20}, snipPoint{900, 700}, &snipRect{0, 0, 800, 600}},
		{"minimum", snipPoint{0, 0}, snipPoint{8, 8}, &snipRect{0, 0, 8, 8}},
		{"narrow", snipPoint{10, 10}, snipPoint{17.9, 50}, nil},
		{"short", snipPoint{10, 10}, snipPoint{50, 17.9}, nil},
		{"rounded", snipPoint{10.5, 20.5}, snipPoint{20.5, 30.5}, &snipRect{11, 21, 10, 10}},
		{"rounded edge", snipPoint{790.5, 580.5}, snipPoint{800, 600}, &snipRect{791, 581, 9, 19}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := snipSelection{width: 800, height: 600}
			s.begin(tt.start.x, tt.start.y)
			got, finished := s.finish(tt.end.x, tt.end.y, false)
			if !finished || (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
				t.Fatalf("selection = %v, %t; want %v", got, finished, tt.want)
			}
			if _, again := s.finish(100, 100, false); again || s.begin(0, 0) || s.cancel() {
				t.Fatal("submitted selection was reused")
			}
		})
	}
}

func snipTestScreen(t *testing.T) (*snipScreen, *ui.Tester, *[]*snipRect) {
	t.Helper()
	var gate entryGate
	results := []*snipRect{}
	screen := &snipScreen{round: &snipRound{task: gate.begin()}}
	screen.submit = func(_ *snipRound, r *snipRect) { results = append(results, r) }
	return screen, ui.NewTester(screen.view, 800, 600), &results
}

func TestNativeSnipDragAndRepeatedFrames(t *testing.T) {
	_, tt, results := snipTestScreen(t)
	tt.Press(110, 90)
	tt.Move(10, 20)
	tt.Frame()
	tt.Release(10, 20)
	for range 5 {
		tt.Frame()
	}
	if len(*results) != 1 || (*results)[0] == nil || *(*results)[0] != (snipRect{10, 20, 100, 70}) {
		t.Fatalf("drag submitted %v", *results)
	}
}

func TestNativeSnipDrawsLightVeilAndTranslucentSelection(t *testing.T) {
	_, tt, _ := snipTestScreen(t)
	idle := tt.Image()
	veil := idle.RGBAAt(20, 20)
	if veil.A == 0 || veil.A > 40 {
		t.Fatalf("idle overlay needs a light veil, got %v", veil)
	}
	for y := range idle.Bounds().Dy() {
		for x := range idle.Bounds().Dx() {
			if idle.RGBAAt(x, y) != veil {
				t.Fatal("idle overlay painted something besides the veil")
			}
		}
	}
	tt.Press(100, 100)
	tt.Move(500, 400)
	img := tt.Image()
	if a := img.RGBAAt(300, 250).A; a <= veil.A || a >= 128 {
		t.Fatalf("selection needs a light translucent fill, got alpha %d", a)
	}
	if img.RGBAAt(20, 20) != veil {
		t.Fatal("dragging changed the veil outside the selection")
	}
	if img.RGBAAt(100, 200).A <= veil.A || img.RGBAAt(110, 95).A <= veil.A {
		t.Fatal("selection border or size badge was not painted")
	}
}

func TestSnipUsedSurfaceIsReplacedBeforeReuse(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		screen, tt, _ := snipTestScreen(t)
		clean := &snipScreen{}
		manager := snipManager{screens: map[int64]*snipScreen{1: screen, 2: clean}}
		if manager.reusableScreen(1) != screen {
			t.Fatal("unused overlay was not reused")
		}
		tt.Press(100, 100)
		tt.Move(500, 400)
		if cancel {
			tt.Key(0, ui.KeyEscape)
		} else {
			tt.Release(500, 400)
		}
		// Hiding clears the model before a clean frame is guaranteed to
		// replace the surface's previous contents.
		screen.round = nil
		closes := 0
		screen.dispose = func() { closes++ }
		if manager.reusableScreen(1) != nil || !screen.disposed || closes != 1 {
			t.Fatal("a surface with an old selection could be shown again")
		}
		if _, retained := manager.screens[1]; retained || manager.reusableScreen(2) != clean {
			t.Fatal("replacement did not preserve the other display's unused overlay")
		}
	}
}

func TestSnipUsedSurfaceKeepsBorrowedHWNDUntilCaptureReturns(t *testing.T) {
	screen, tt, _ := snipTestScreen(t)
	tt.Press(100, 100)
	tt.Release(500, 400)
	screen.round = nil
	screen.captures = 1
	closes := 0
	screen.dispose = func() { closes++ }
	manager := snipManager{screens: map[int64]*snipScreen{1: screen}}
	if manager.reusableScreen(1) != nil || !screen.retired || closes != 0 {
		t.Fatal("replacement destroyed a borrowed HWND")
	}
	screen.releaseCapture()
	if closes != 1 {
		t.Fatal("capture completion did not dispose the old surface")
	}
}

func TestNativeSnipCaptureClampsOutsideDisplay(t *testing.T) {
	_, tt, results := snipTestScreen(t)
	tt.Press(10, 10)
	tt.Move(-100, -100)
	tt.Release(900, 700)
	if len(*results) != 1 || (*results)[0] == nil || *(*results)[0] != (snipRect{10, 10, 790, 590}) {
		t.Fatalf("captured pointer did not clamp to its display: %v", *results)
	}
}

func TestNativeSnipCancellation(t *testing.T) {
	for _, reason := range []string{"escape", "right", "capture", "blur", "small"} {
		t.Run(reason, func(t *testing.T) {
			_, tt, results := snipTestScreen(t)
			tt.Press(10, 10)
			tt.Move(150, 120)
			switch reason {
			case "escape":
				tt.Key(0, ui.KeyEscape)
			case "right":
				tt.PressButton(1, 150, 120)
			case "capture":
				tt.CancelPress()
			case "blur":
				tt.SetFocused(false)
			case "small":
				tt.Move(17, 17)
				tt.Release(17, 17)
			}
			tt.Release(150, 120)
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			if len(*results) != 1 || (*results)[0] != nil {
				t.Fatalf("%s committed a canceled gesture: %v", reason, *results)
			}
		})
	}
}

func TestNativeSnipIdleCancellation(t *testing.T) {
	for _, right := range []bool{false, true} {
		_, tt, results := snipTestScreen(t)
		if right {
			tt.PressButton(1, 20, 20)
		} else {
			tt.Key(0, ui.KeyEscape)
		}
		if len(*results) != 1 || (*results)[0] != nil {
			t.Fatal("idle cancellation did not submit once")
		}
	}
}

func TestNativeSnipRoundReplacementRejectsOldInput(t *testing.T) {
	screen, tt, results := snipTestScreen(t)
	old := screen.round
	tt.Press(10, 10)
	tt.Move(100, 100)
	next := old.task.gate.begin()
	screen.round = &snipRound{task: next}
	tt.Frame() // Removing the pressed element delivers a canceled old release.
	screen.input(old, ui.InputEvent{Kind: ui.InputPointerUp, Button: 0, X: 200, Y: 200})
	if len(*results) != 0 || screen.round.selection.submitted {
		t.Fatal("old input submitted or canceled the replacement round")
	}
	tt.Press(50, 50)
	tt.Release(150, 150)
	if len(*results) != 1 || (*results)[0] == nil {
		t.Fatal("replacement round did not accept a fresh gesture")
	}
	// A canceled task cannot accept events even before its next view frame.
	screen.round = &snipRound{task: next}
	next.cancel()
	screen.input(screen.round, ui.InputEvent{Kind: ui.InputPointerDown, Button: 0, X: 10, Y: 10})
	if screen.round.selection.dragging {
		t.Fatal("canceled entry began a drag")
	}
}

func TestNativeSnipHiddenRoundCannotSubmit(t *testing.T) {
	screen, tt, results := snipTestScreen(t)
	tt.Press(10, 10)
	screen.round = nil
	tt.Frame()
	tt.Release(150, 150)
	if len(*results) != 0 {
		t.Fatal("a hidden round submitted its canceled release")
	}
	var gate entryGate
	screen.round = &snipRound{task: gate.begin()}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if len(*results) != 1 || (*results)[0] != nil {
		t.Fatal("a returned round did not recover keyboard input")
	}
}

func TestSnipTaskAndWindowOwnership(t *testing.T) {
	var gate entryGate
	old := gate.begin()
	current := gate.begin()
	win, other := &mygo.Window{}, &mygo.Window{}
	screen := &snipScreen{win: win}
	s := snipManager{task: current, screens: map[int64]*snipScreen{1: screen}}
	if task, _ := s.take(old.id, win); task != nil || s.task != current {
		t.Fatal("stale task consumed the new shot")
	}
	if task, _ := s.take(current.id, other); task != nil || s.task != current {
		t.Fatal("foreign window consumed the shot")
	}
	if task, owner := s.take(current.id, win); task != current || owner != screen || s.task != nil {
		t.Fatal("current screen could not consume its shot")
	}
	if task, _ := s.take(current.id, win); task != nil {
		t.Fatal("duplicate completion consumed a shot twice")
	}
}

func TestSnipDisplayChangesRetireAfterCapture(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		display := mygo.Display{ID: 7, Bounds: mygo.Rectangle{X: -1920, Y: -100, Width: 1920, Height: 1080}, ScaleFactor: scale}
		closes := 0
		var gate entryGate
		task := gate.begin()
		screen := &snipScreen{display: display, captures: 1, captureTask: task, dispose: func() { closes++ }}
		s := snipManager{screens: map[int64]*snipScreen{7: screen}}
		s.prune([]mygo.Display{display})
		if screen.retired || closes != 0 {
			t.Fatal("unchanged display was not reused")
		}
		changed := display
		changed.Bounds.X = 0
		s.prune([]mygo.Display{changed})
		if !screen.retired || len(s.screens) != 0 || closes != 0 || task.valid() {
			t.Fatal("display change destroyed an in-flight capture HWND")
		}
		screen.releaseCapture()
		screen.retire()
		if closes != 1 {
			t.Fatal("retired capture did not close exactly once")
		}
		removed := &snipScreen{display: display, dispose: func() { closes++ }}
		s.screens[display.ID] = removed
		s.prune(nil)
		if !removed.disposed || closes != 2 || len(s.screens) != 0 {
			t.Fatal("disconnected display was retained")
		}
	}
}
