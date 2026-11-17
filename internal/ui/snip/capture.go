package snip

import (
	"context"
	"image"
	"image/draw"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sotto/internal/capture"
)

type snapshotFrame struct {
	image   image.Image
	bitmap  *ui.Bitmap
	display mygo.Rectangle
}

// inContent places the display snapshot in overlay-local logical coordinates.
// The display and overlay may have different origins at a screen edge.
func (f *snapshotFrame) inContent(content mygo.Rectangle) ui.Rect {
	return ui.Rect{X: float32(f.display.X - content.X), Y: float32(f.display.Y - content.Y), W: float32(f.display.Width), H: float32(f.display.Height)}
}

// pixels uses the same display-to-content projection as the painted background.
func (f *snapshotFrame) pixels(content mygo.Rectangle, selection Rect) image.Rectangle {
	area := f.inContent(content)
	bounds := f.image.Bounds()
	edge := func(local int, origin, logicalSize float32, pixels int) int {
		return int(math.Floor((float64(local)-float64(origin))*float64(pixels)/float64(logicalSize) + .5))
	}
	return image.Rect(
		edge(selection.X, area.X, area.W, bounds.Dx()),
		edge(selection.Y, area.Y, area.H, bounds.Dy()),
		edge(selection.X+selection.W, area.X, area.W, bounds.Dx()),
		edge(selection.Y+selection.H, area.Y, area.H, bounds.Dy()),
	).Add(bounds.Min).Intersect(bounds)
}

// Capture retains only the selected display's pixels until OCR receives its crop.
type Capture struct {
	Task   Task
	Region mygo.Rectangle // global logical bounds for placing the translation
	image  image.Image
	pixels image.Rectangle
}

func (s *Manager) finish(win *mygo.Window, sessionID int64, rect *Rect) {
	task, screen := s.take(sessionID, win)
	if task == nil {
		return
	}
	round := screen.round
	region, valid := snipCaptureRegion(round.content, rect)
	if !valid || s.Done == nil {
		s.Hide()
		task.Cancel()
		return
	}
	shot := &Capture{
		Task: task, Region: region,
		image: round.frame.image, pixels: round.frame.pixels(round.content, *rect),
	}
	s.Hide()
	s.Done(shot)
}

// Grab is called once by the OCR worker. Only the crop remains live afterwards.
func (c *Capture) Grab() (image.Image, error) {
	if !c.Task.Valid() {
		c.image = nil
		return nil, context.Canceled
	}
	return c.Crop(c.Task.Context())
}

// Crop also accepts a job that has taken ownership of a submitted snapshot.
func (c *Capture) Crop(ctx context.Context) (image.Image, error) {
	source := c.image
	c.image = nil
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	width, height := c.pixels.Dx(), c.pixels.Dy()
	if err := capture.ValidateSelection(width, height); err != nil {
		return nil, err
	}
	crop := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(crop, crop.Bounds(), source, c.pixels.Min, draw.Src)
	return crop, nil
}
