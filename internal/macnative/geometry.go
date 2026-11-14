package macnative

import (
	"fmt"
	"math"
	"structs"
)

// HostLayout describes structures passed by value through the native bridge.
type Point struct {
	_    structs.HostLayout
	X, Y float64
}
type Size struct {
	_             structs.HostLayout
	Width, Height float64
}
type Rect struct {
	_      structs.HostLayout
	Origin Point
	Size   Size
}

func NewRect(x, y, w, h float64) Rect {
	return Rect{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

// CocoaRect flips around the primary display, not the union of all displays.
func CocoaRect(r Rect, primaryHeight float64) Rect {
	return NewRect(r.Origin.X, primaryHeight-r.Origin.Y-r.Size.Height, r.Size.Width, r.Size.Height)
}

func CaptureDimensions(r Rect, scale float64, maxSide, maxPixels int) (int, int, error) {
	for _, v := range []float64{r.Origin.X, r.Origin.Y, r.Size.Width, r.Size.Height, scale} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, fmt.Errorf("non-finite capture geometry")
		}
	}
	if r.Origin.X < 0 || r.Origin.Y < 0 || r.Size.Width <= 0 || r.Size.Height <= 0 || scale <= 0 || scale > 8 {
		return 0, 0, fmt.Errorf("invalid capture geometry")
	}
	// Round both edges so neighboring selections share an edge.
	w := math.Round((r.Origin.X+r.Size.Width)*scale) - math.Round(r.Origin.X*scale)
	h := math.Round((r.Origin.Y+r.Size.Height)*scale) - math.Round(r.Origin.Y*scale)
	if w < 1 || h < 1 || w > float64(maxSide) || h > float64(maxSide) || w*h > float64(maxPixels) {
		return 0, 0, fmt.Errorf("capture exceeds pixel budget")
	}
	return int(w), int(h), nil
}
