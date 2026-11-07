package main

import (
	"github.com/egoist/mygo"
	"testing"
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
			p := newBubblePlacement(work, tt.point, tt.input, 480)
			x, y := p.position(480, 600)
			if x != tt.wantX || y != tt.wantY {
				t.Fatalf("position = %d,%d; want %d,%d", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestBubbleGrowsAwayFromBottomAnchor(t *testing.T) {
	work := mygo.Rectangle{Width: 1920, Height: 1040}
	p := newBubblePlacement(work, mygo.Point{X: 1000, Y: 980}, false, 480)
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
			p := newBubblePlacement(work, point, false, 480)
			for _, h := range []int{160, 240, height} {
				x, y := p.position(480, h)
				if x < work.X || x+480 > work.X+work.Width || y < work.Y+12 || y+h > work.Y+work.Height-12 {
					t.Fatalf("bubble %d,%d 480x%d outside %+v", x, y, h, work)
				}
			}
		}
	}
}
