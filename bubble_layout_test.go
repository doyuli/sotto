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
