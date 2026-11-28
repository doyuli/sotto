//go:build darwin

package selection

import (
	"context"
	"errors"

	"github.com/egoist/mygo"

	"sotto/internal/macnative"
)

// A drag may end on the trailing edge of the selected text.
func selectionBoundsContainRelease(r Rect, p mygo.Point) bool {
	return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H
}

func readAXPointSelection(ctx context.Context, app uintptr, point mygo.Point) (Selection, error) {
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	// Chromium can enable native accessibility lazily when a client reads
	// the application's role. Read it before asking for the element under the pointer.
	readAXString(app, "AXRole")
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	var hit uintptr
	if axElementAtPosition(app, float32(point.X), float32(point.Y), &hit) != 0 || hit == 0 {
		return Selection{}, ErrNoSelection
	}
	return readSelectionAncestors(ctx, hit, readAXDocumentSelection, func(element uintptr) uintptr {
		return copyAXElement(element, "AXParent")
	}, cfRelease)
}

func readAXDocumentSelection(ctx context.Context, element uintptr) (Selection, error) {
	sel, err := readAXElementSelection(ctx, element)
	if err == nil {
		sel.Rect, sel.HasRect = readAXSelectionBounds(element)
		return sel, nil
	}
	if !errors.Is(err, ErrNoSelection) {
		return sel, err
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	if readAXString(element, "AXRole") != "AXWebArea" {
		return Selection{}, ErrNoSelection
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	// WebKit and Chromium expose document selections as text markers,
	// even where the ordinary selected-text attribute is unavailable.
	marker := copyAXAttribute(element, "AXSelectedTextMarkerRange")
	if marker == 0 {
		return Selection{}, ErrNoSelection
	}
	defer cfRelease(marker)
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	var value uintptr
	if axParameter(element, macnative.String("AXStringForTextMarkerRange"), marker, &value) != 0 || value == 0 {
		return Selection{}, ErrNoSelection
	}
	defer cfRelease(value)
	if text := axString(value); text != "" {
		return Selection{Text: text, Via: "ax"}, nil
	}
	return Selection{}, ErrNoSelection
}
