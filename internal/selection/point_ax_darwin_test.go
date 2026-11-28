//go:build darwin

package selection

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestSelectionBoundsContainRelease(t *testing.T) {
	// A focused pane can still report its old selection after a drag in another pane.
	r := Rect{X: -80, Y: 20, W: 60, H: 16}
	if selectionBoundsContainRelease(r, mygo.Point{X: 40, Y: 25}) || !selectionBoundsContainRelease(r, mygo.Point{X: -20, Y: 36}) {
		t.Fatal("focused selection must cover the release point, including the trailing edge")
	}
}
