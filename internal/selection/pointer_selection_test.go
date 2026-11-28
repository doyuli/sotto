package selection

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestPointerSelectionReadsDocumentAncestor(t *testing.T) {
	// A browser's hit-tested text leaf has no selection attribute; the
	// document ancestor owns the text range selected across leaves.
	secureField := errors.New("secure field")
	const leaf, group, document = 1, 2, 3
	for _, tc := range []struct {
		name         string
		text         string
		wantErr      error
		wantReleased []uintptr
	}{
		{"document selection", "selected page text", nil, []uintptr{leaf, group, document}},
		{"collapsed range", "", ErrNoSelection, []uintptr{leaf, group, document}},
		{"secure field", "", secureField, []uintptr{leaf}},
		{"interrupted", "", context.Canceled, []uintptr{leaf}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var released []uintptr
			got, err := readSelectionAncestors(ctx, uintptr(leaf), func(_ context.Context, element uintptr) (Selection, error) {
				if tc.wantErr == secureField {
					return Selection{}, secureField
				}
				if tc.wantErr == context.Canceled {
					cancel()
				}
				if element == document && tc.text != "" {
					return Selection{Text: tc.text, Via: "ax"}, nil
				}
				return Selection{}, ErrNoSelection
			}, func(element uintptr) uintptr {
				return map[uintptr]uintptr{leaf: group, group: document}[element]
			}, func(element uintptr) { released = append(released, element) })
			if !errors.Is(err, tc.wantErr) || got.Text != tc.text || !slices.Equal(released, tc.wantReleased) {
				t.Fatalf("selection = %+v, error = %v, released = %v", got, err, released)
			}
		})
	}
}
