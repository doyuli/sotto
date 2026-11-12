package main

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestBubblePlacement(t *testing.T) {
	work := mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1040}
	for _, tt := range []struct {
		name         string
		point        mygo.Point
		input        bool
		wantX, wantY int
	}{
		{"selection near top", mygo.Point{X: 100, Y: 100}, false, 116, 116},
		{"bottom right flips both axes", mygo.Point{X: 1900, Y: 1000}, false, 1404, 384},
		{"top left keeps margin", mygo.Point{X: 0, Y: 0}, false, 16, 16},
		{"input centered", mygo.Point{X: 1900, Y: 1000}, true, 720, 260},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newBubblePlacement(mygo.Display{ID: 1, WorkArea: work}, tt.point, tt.input, 480)
			x, y := p.position(480, 600)
			if x != tt.wantX || y != tt.wantY {
				t.Fatalf("position = %d,%d; want %d,%d", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestBubbleGrowsAwayFromBottomAnchor(t *testing.T) {
	work := mygo.Rectangle{Width: 1920, Height: 1040}
	p := newBubblePlacement(mygo.Display{ID: 1, WorkArea: work}, mygo.Point{X: 1000, Y: 980}, false, 480)
	_, shortY := p.position(480, 240)
	_, tallY := p.position(480, 760)
	if shortY+240 != tallY+760 {
		t.Fatal("growth moved the bottom anchor")
	}
	// Shrinking returns to the original side instead of sticking to the top edge.
	_, again := p.position(480, 240)
	if again != shortY {
		t.Fatalf("shrink returned to %d; want %d", again, shortY)
	}
}

func TestBubbleWorkAreas(t *testing.T) {
	for _, work := range []mygo.Rectangle{
		{X: -1920, Y: -200, Width: 1920, Height: 1040},
		{X: 100, Y: 80, Width: 800, Height: 560},
		{X: -320, Y: 0, Width: 480, Height: 480},
	} {
		height := bubbleHeightLimit(work)
		if height > 760 || height > work.Height-24 {
			t.Fatalf("height %d exceeds work area %+v", height, work)
		}
		for _, point := range []mygo.Point{
			{X: work.X, Y: work.Y},
			{X: work.X + work.Width - 1, Y: work.Y + work.Height - 1},
			{X: work.X + work.Width/2, Y: work.Y + work.Height/2},
		} {
			p := newBubblePlacement(mygo.Display{ID: 1, WorkArea: work}, point, false, 480)
			for _, h := range []int{160, 240, height} {
				x, y := p.position(480, h)
				if x < work.X || x+480 > work.X+work.Width || y < work.Y+12 || y+h > work.Y+work.Height-12 {
					t.Fatalf("bubble %d,%d 480x%d outside %+v", x, y, h, work)
				}
			}
		}
	}
}

func TestBubblePlacementRefreshAfterDisplayChanges(t *testing.T) {
	secondary := mygo.Display{ID: 2, WorkArea: mygo.Rectangle{X: 1920, Width: 1920, Height: 1040}}
	primary := mygo.Display{ID: 1, WorkArea: mygo.Rectangle{Width: 1920, Height: 680}}
	for _, tt := range []struct {
		name     string
		input    bool
		displays []mygo.Display
		fallback mygo.Display
		want     mygo.Display
	}{
		{"hidden input draft after unplug", true, []mygo.Display{primary}, primary, primary},
		{"selection after unplug", false, []mygo.Display{primary}, primary, primary},
		{"work area shrinks", false, []mygo.Display{{ID: 2, WorkArea: mygo.Rectangle{X: 1920, Y: 40, Width: 1280, Height: 600}}}, primary,
			mygo.Display{ID: 2, WorkArea: mygo.Rectangle{X: 1920, Y: 40, Width: 1280, Height: 600}}},
		{"display moves to negative coordinates", true, []mygo.Display{{ID: 2, WorkArea: mygo.Rectangle{X: -1600, Y: -200, Width: 1600, Height: 860}}}, primary,
			mygo.Display{ID: 2, WorkArea: mygo.Rectangle{X: -1600, Y: -200, Width: 1600, Height: 860}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newBubblePlacement(secondary, mygo.Point{X: 3700, Y: 1000}, tt.input, 480)
			p.refresh(tt.displays, tt.fallback, 480)
			if p.displayID != tt.want.ID || p.work != tt.want.WorkArea || p.input != tt.input {
				t.Fatalf("refreshed placement = %+v; want display %+v, input %t", p, tt.want, tt.input)
			}
			work := tt.want.WorkArea
			if p.point.X < work.X || p.point.X >= work.X+work.Width || p.point.Y < work.Y || p.point.Y >= work.Y+work.Height {
				t.Fatalf("stale anchor %+v outside %+v", p.point, work)
			}
			for _, height := range []int{160, bubbleHeightLimit(work)} {
				x, y := p.position(480, height)
				if x < work.X+12 || x+480 > work.X+work.Width-12 || y < work.Y+12 || y+height > work.Y+work.Height-12 {
					t.Fatalf("refreshed bubble %d,%d 480x%d outside %+v", x, y, height, work)
				}
			}
			if tt.input {
				x, _ := p.position(480, 160)
				if wantX := work.X + (work.Width-480)/2; x != wantX {
					t.Fatalf("draft x = %d; want centered at %d", x, wantX)
				}
			}
			before := p
			p.refresh(tt.displays, tt.fallback, 480)
			if p != before {
				t.Fatal("unchanged displays moved the refreshed anchor")
			}
		})
	}
}

func TestBubblePlacementKeepsLiveAnchorDisplayDuringGrowth(t *testing.T) {
	anchorDisplay := mygo.Display{ID: 1, WorkArea: mygo.Rectangle{Width: 1920, Height: 1040}}
	below := mygo.Display{ID: 2, WorkArea: mygo.Rectangle{Y: 1040, Width: 1920, Height: 1040}}
	p := newBubblePlacement(anchorDisplay, mygo.Point{X: 1000, Y: 1000}, false, 480)
	before := p
	// The window center can temporarily cross into the display below before
	// the resize callback moves a growing bubble up around its bottom anchor.
	p.refresh([]mygo.Display{anchorDisplay, below}, below, 480)
	if p != before {
		t.Fatal("content growth switched a still-connected anchor display")
	}
}

func TestBubblePositionDistinguishesManualMoves(t *testing.T) {
	primary := mygo.Display{ID: 1, WorkArea: mygo.Rectangle{Width: 1920, Height: 1040}, ScaleFactor: 1}
	secondary := mygo.Display{ID: 2, WorkArea: mygo.Rectangle{X: 1920, Width: 1280, Height: 680}, ScaleFactor: 1.5}
	displays := []mygo.Display{primary, secondary}
	start := mygo.Rectangle{X: 116, Y: 116, Width: 480, Height: 240}
	for _, tt := range []struct {
		name     string
		bounds   mygo.Rectangle
		displays []mygo.Display
		want     bool
	}{
		{"duplicate or delayed move event", start, displays, false},
		{"content resize only", mygo.Rectangle{X: 116, Y: 116, Width: 480, Height: 760}, displays, false},
		{"manual move on same display", mygo.Rectangle{X: 600, Y: 300, Width: 480, Height: 240}, displays, true},
		{"manual move across displays and DPI", mygo.Rectangle{X: 2100, Y: 200, Width: 480, Height: 240}, displays, true},
		{"display enumeration order changes", mygo.Rectangle{X: 600, Y: 300, Width: 480, Height: 240}, []mygo.Display{secondary, primary}, true},
		{"display removed relocates window", mygo.Rectangle{X: 600, Y: 300, Width: 480, Height: 240}, []mygo.Display{primary}, false},
		{"work area changes relocate window", mygo.Rectangle{X: 600, Y: 300, Width: 480, Height: 240}, []mygo.Display{{ID: 1, WorkArea: mygo.Rectangle{Width: 1920, Height: 680}, ScaleFactor: 1}, secondary}, false},
		{"DPI configuration changes relocate window", mygo.Rectangle{X: 600, Y: 300, Width: 480, Height: 240}, []mygo.Display{{ID: 1, WorkArea: primary.WorkArea, ScaleFactor: 1.25}, secondary}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var p bubblePosition
			p.remember(start, displays)
			if got := p.moved(tt.bounds, tt.displays); got != tt.want {
				t.Fatalf("manual move = %t; want %t", got, tt.want)
			}
			if p.moved(tt.bounds, tt.displays) {
				t.Fatal("duplicate notification was treated as another manual move")
			}
		})
	}
}

func TestBubblePositionProgrammaticMovesDoNotConsumeNextDrag(t *testing.T) {
	displays := []mygo.Display{{ID: 1, WorkArea: mygo.Rectangle{Width: 1920, Height: 1040}}}
	var p bubblePosition
	initial := mygo.Rectangle{X: 116, Y: 116, Width: 480, Height: 240}
	if p.moved(initial, displays) {
		t.Fatal("first observation was treated as a manual move")
	}
	// position and reflow record actual native bounds before callbacks arrive.
	positioned := mygo.Rectangle{X: 900, Y: 500, Width: 480, Height: 240}
	p.remember(positioned, displays)
	if p.moved(positioned, displays) {
		t.Fatal("delayed positioning event discarded the anchor")
	}
	reflowed := mygo.Rectangle{X: 900, Y: 200, Width: 480, Height: 760}
	p.remember(reflowed, displays)
	if p.moved(reflowed, displays) {
		t.Fatal("delayed reflow event discarded the anchor")
	}
	// No event-count suppression: even if an old native event arrives first,
	// its callback reads the current geometry and must notice the user's drag.
	dragged := mygo.Rectangle{X: 600, Y: 100, Width: 480, Height: 760}
	if !p.moved(dragged, displays) {
		t.Fatal("pending programmatic event swallowed the next manual move")
	}
}

func TestBubbleManualPositionSurvivesContentHeightChanges(t *testing.T) {
	work := mygo.Rectangle{X: 1920, Width: 1280, Height: 680}
	x, y := 2100, 200
	for _, height := range []int{240, 160, 400, 160} {
		x, y = clampBubblePosition(work, x, y, 480, height)
		if x != 2100 || y != 200 {
			t.Fatalf("height %d moved manual position to %d,%d", height, x, y)
		}
	}
	// Growth may clamp to the current monitor, never back to the old anchor.
	height := bubbleHeightLimit(work)
	x, y = clampBubblePosition(work, x, y, 480, height)
	if x != 2100 || y != bubbleMargin || height != 656 {
		t.Fatalf("monitor-constrained position = %d,%d height %d", x, y, height)
	}
	if nextX, nextY := clampBubblePosition(work, x, y, 480, 160); nextX != x || nextY != y {
		t.Fatal("folding restored an old anchor after clamped growth")
	}
}

func TestBubbleMeasuredHeightUsesNativeUnitsAndWorkArea(t *testing.T) {
	for _, tt := range []struct {
		name          string
		height, limit float64
		want          int
	}{
		{"native height", 200, 760, 200},
		{"fractional height", 200.5, 760, 201},
		{"work area", 500, 300, 300},
		{"fractional work area", 500, 300.5, 301},
		{"minimum", 80, 760, 160},
		{"maximum", 1e100, 760, 760},
		{"absolute maximum", 1e100, 1000, 760},
		{"rounding tolerance", 200.001, 760, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := bubbleLayoutHeight(tt.height, tt.limit); got != tt.want {
				t.Fatalf("height = %d; want %d", got, tt.want)
			}
		})
	}
}
