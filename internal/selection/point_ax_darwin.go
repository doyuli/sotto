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
	return readAXSelectionAncestors(ctx, hit, readAXDocumentSelection, func(element uintptr) uintptr {
		return copyAXElement(element, "AXParent")
	}, cfRelease)
}

// The mouse can select text in web content while keyboard focus stays in
// another control. A leaf hit by the pointer may delegate selection to its document.
func readAXSelectionAncestors(ctx context.Context, element uintptr, read func(context.Context, uintptr) (Selection, error), parent func(uintptr) uintptr, release func(uintptr)) (Selection, error) {
	const maxAncestors = 24
	for depth := 0; element != 0 && depth < maxAncestors; depth++ {
		if err := ctx.Err(); err != nil {
			release(element)
			return Selection{}, err
		}
		sel, err := read(ctx, element)
		if !errors.Is(err, ErrNoSelection) {
			release(element)
			return sel, err
		}
		next := uintptr(0)
		if depth+1 < maxAncestors && ctx.Err() == nil {
			next = parent(element)
		}
		release(element)
		element = next
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	return Selection{}, ErrNoSelection
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
