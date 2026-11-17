package snip

import (
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestSnipSelectionGeometry(t *testing.T) {
	for _, tt := range []struct {
		name       string
		start, end snipPoint
		want       *Rect
	}{
		{"forward", snipPoint{10, 20}, snipPoint{110, 70}, &Rect{10, 20, 100, 50}},
		{"reverse", snipPoint{110, 70}, snipPoint{10, 20}, &Rect{10, 20, 100, 50}},
		{"clamped", snipPoint{-10, -20}, snipPoint{900, 700}, &Rect{0, 0, 800, 600}},
		{"minimum", snipPoint{0, 0}, snipPoint{8, 8}, &Rect{0, 0, 8, 8}},
		{"narrow", snipPoint{10, 10}, snipPoint{17.9, 50}, nil},
		{"short", snipPoint{10, 10}, snipPoint{50, 17.9}, nil},
		{"rounded", snipPoint{10.5, 20.5}, snipPoint{20.5, 30.5}, &Rect{11, 21, 10, 10}},
		{"rounded edge", snipPoint{790.5, 580.5}, snipPoint{800, 600}, &Rect{791, 581, 9, 19}},
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

func TestSnipCaptureUsesClientBounds(t *testing.T) {
	content := mygo.Rectangle{X: -1912, Y: -92, Width: 1904, Height: 1064}
	for _, tt := range []struct {
		name string
		rect *Rect
		want *mygo.Rectangle
	}{
		{"client origin", &Rect{0, 0, 8, 8}, &mygo.Rectangle{X: -1912, Y: -92, Width: 8, Height: 8}},
		{"bottom right", &Rect{1896, 1056, 8, 8}, &mygo.Rectangle{X: -16, Y: 964, Width: 8, Height: 8}},
		{"beyond client width", &Rect{1900, 0, 8, 8}, nil},
		{"beyond client height", &Rect{0, 1060, 8, 8}, nil},
		{"outside origin", &Rect{-1, 0, 8, 8}, nil},
		{"too small", &Rect{0, 0, 7, 8}, nil},
		{"cancel", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, valid := snipCaptureRegion(content, tt.rect)
			if valid != (tt.want != nil) || valid && got != *tt.want {
				t.Fatalf("capture region = %+v, valid=%t; want %v", got, valid, tt.want)
			}
		})
	}
}

func snipTestScreen(t *testing.T) (*snipScreen, *ui.Tester, *[]*Rect) {
	t.Helper()
	var gate testGate
	results := []*Rect{}
	screen := &snipScreen{round: &snipRound{task: gate.Begin()}}
	screen.submit = func(_ *snipRound, r *Rect) { results = append(results, r) }
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
	if len(*results) != 1 || (*results)[0] == nil || *(*results)[0] != (Rect{10, 20, 100, 70}) {
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

func TestNativeSnipCaptureClampsOutsideDisplay(t *testing.T) {
	_, tt, results := snipTestScreen(t)
	tt.Press(10, 10)
	tt.Move(-100, -100)
	tt.Release(900, 700)
	if len(*results) != 1 || (*results)[0] == nil || *(*results)[0] != (Rect{10, 10, 790, 590}) {
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
	old.task.Cancel()
	next := (&testGate{}).Begin()
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
	var gate testGate
	screen.round = &snipRound{task: gate.Begin()}
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if len(*results) != 1 || (*results)[0] != nil {
		t.Fatal("a returned round did not recover keyboard input")
	}
}

func TestSnipTaskAndWindowOwnership(t *testing.T) {
	var gate testGate
	old := gate.Begin()
	current := gate.Begin()
	win, other := &mygo.Window{}, &mygo.Window{}
	screen := &snipScreen{win: win, round: &snipRound{task: current}}
	s := Manager{task: current, screens: map[int64]*snipScreen{1: screen}}
	if task, _ := s.take(old.ID(), win); task != nil || s.task != current {
		t.Fatal("stale task consumed the new shot")
	}
	if task, _ := s.take(current.ID(), other); task != nil || s.task != current {
		t.Fatal("foreign window consumed the shot")
	}
	if task, owner := s.take(current.ID(), win); task != current || owner != screen || s.task != nil {
		t.Fatal("current screen could not consume its shot")
	}
	if task, _ := s.take(current.ID(), win); task != nil {
		t.Fatal("duplicate completion consumed a shot twice")
	}
}
