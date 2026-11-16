package snip

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestSnipUsesDisplayedSnapshotForOCR(t *testing.T) {
	// A menu can disappear after the overlay takes focus. OCR must receive
	// the original pixels, with the same offset and scale used for display.
	var gate testGate
	task := gate.Begin()
	display := mygo.Rectangle{X: -200, Y: -50, Width: 120, Height: 100}
	content := mygo.Rectangle{X: -200, Y: -30, Width: 120, Height: 100}
	img := image.NewRGBA(image.Rect(0, 0, 240, 200))
	background, menu := color.RGBA{B: 230, A: 255}, color.RGBA{R: 230, A: 255}
	draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(20, 60, 100, 100), image.NewUniform(menu), image.Point{}, draw.Src)
	frame := &snapshotFrame{image: img, bitmap: ui.NewBitmap(img), display: display}
	screen := &snipScreen{round: &snipRound{task: task, frame: frame, content: content}}
	var crop image.Image
	var err error
	screen.submit = func(round *snipRound, selected *Rect) {
		if selected != nil {
			shot := &Capture{Task: round.task, image: frame.image, pixels: frame.pixels(round.content, *selected)}
			crop, err = shot.Grab()
		}
	}
	tt := ui.NewTester(screen.view, content.Width, content.Height)
	preview := tt.Image()
	if inside, outside := preview.RGBAAt(30, 20), preview.RGBAAt(30, 45); inside.R <= inside.B || outside.B <= outside.R {
		t.Fatalf("preview did not place the menu at the expected offset: inside %v outside %v", inside, outside)
	}
	tt.Press(10, 10)
	tt.Move(50, 30)
	tt.Release(50, 30)
	if err != nil || crop == nil || crop.Bounds() != image.Rect(0, 0, 80, 40) {
		t.Fatalf("menu crop: %v, %v", crop, err)
	}
	for y := range 40 {
		for x := range 80 {
			if c := crop.At(x, y); c != menu {
				t.Fatalf("OCR pixel (%d, %d) = %v, want %v", x, y, c, menu)
			}
		}
	}
}

func TestSnipSnapshotClipsRoundedEdges(t *testing.T) {
	// The client window can extend past the snapshot on a mixed-DPI desktop.
	frame := snapshotFrame{image: image.NewRGBA(image.Rect(0, 0, 150, 150)), display: mygo.Rectangle{Width: 100, Height: 100}}
	content := mygo.Rectangle{Width: 101, Height: 101}
	if got := frame.pixels(content, Rect{1, 1, 100, 100}); got != image.Rect(2, 2, 150, 150) {
		t.Fatalf("clipped physical crop = %v", got)
	}
}

func TestCanceledCaptureRejectsSnapshot(t *testing.T) {
	var gate testGate
	task := gate.Begin()
	gate.Begin()
	shot := &Capture{Task: task}
	if img, err := shot.Grab(); img != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture: image=%v err=%v", img, err)
	}
}

func TestSnipDisplayChangeRevokesPendingSnapshot(t *testing.T) {
	var gate testGate
	pending := gate.Begin()
	manager := Manager{task: pending}
	manager.RefreshDisplays()
	if pending.Valid() || manager.task != nil {
		t.Fatal("display change left pending screenshot active")
	}
	current := gate.Begin()
	manager.task = current
	if manager.acceptSnapshot(pending, 1, 1) {
		t.Fatal("late snapshot was accepted")
	}
	if manager.task != current || !current.Valid() {
		t.Fatal("late snapshot affected the new screenshot")
	}
}

func TestSnipForegroundChangeDiscardsPendingSnapshot(t *testing.T) {
	// A slow screenshot used to interrupt the app the user switched to.
	var gate testGate
	pending := gate.Begin()
	manager := Manager{task: pending}
	if !manager.acceptSnapshot(pending, 10, 10) {
		t.Fatal("unchanged foreground rejected the pending snapshot")
	}
	if manager.acceptSnapshot(pending, 10, 20) || pending.Valid() || manager.task != nil {
		t.Fatal("switching foreground while grabbing left the screenshot active")
	}
}

func TestSnipEscapeCancelsPendingSnapshot(t *testing.T) {
	// Escape used to depend on an overlay, which did not exist while
	// capture was blocked. A late result must still be rejected.
	var gate testGate
	var escape func()
	registered := false
	manager := Manager{RegisterEscape: func(fn func()) (func(), error) {
		if registered {
			t.Fatal("previous Escape registration is still active")
		}
		escape, registered = fn, true
		return func() { registered = false }, nil
	}}
	pending := gate.Begin()
	manager.beginSnapshot(pending)
	staleEscape := escape
	started, canceled := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		<-pending.Context().Done()
		close(canceled)
	}()
	defer pending.Cancel()
	<-started
	escape()
	if pending.Valid() || registered || manager.acceptSnapshot(pending, 1, 1) {
		t.Fatal("Escape left a pending snapshot or shortcut active")
	}
	<-canceled

	current := gate.Begin()
	t.Cleanup(current.Cancel)
	manager.beginSnapshot(current)
	staleEscape()
	if !registered || !manager.acceptSnapshot(current, 1, 1) || registered {
		t.Fatal("the replacement snapshot did not hand Escape back to the overlay")
	}
	escape()
	if !current.Valid() {
		t.Fatal("a queued global Escape canceled the overlay after handoff")
	}
}

func TestSnipClosingDropsFrameBeforeRendererDetaches(t *testing.T) {
	// A paint callback or listener can keep an old round alive after hiding.
	round := &snipRound{frame: &snapshotFrame{image: image.NewRGBA(image.Rect(0, 0, 10, 10))}}
	screen := &snipScreen{round: round}
	screen.clearRound()
	if screen.round != nil || round.frame != nil {
		t.Fatal("ended round still owns the display snapshot")
	}
}
