//go:build darwin

package selection

import (
	"context"
	"errors"
	"testing"
)

func TestMacSelectionFallbackAndPermission(t *testing.T) {
	denied := errors.New("permission denied")
	for _, tc := range []struct {
		text                   string
		readErr, permissionErr error
		copied                 bool
		wantErr                error
	}{
		{text: "selected"}, {readErr: ErrNoSelection, copied: true}, {copied: true}, {permissionErr: denied, wantErr: denied}, {readErr: ErrSourceChanged, wantErr: ErrSourceChanged},
	} {
		copied := false
		_, err := readMacSelection(context.Background(), func(ctx context.Context) error { return ctx.Err() }, func(bool) error { return tc.permissionErr }, func(context.Context) (Selection, error) { return Selection{Text: tc.text}, tc.readErr }, func(context.Context) (Selection, error) { copied = true; return Selection{Text: "copy"}, nil })
		if copied != tc.copied || !errors.Is(err, tc.wantErr) {
			t.Fatalf("%+v: copied=%t err=%v", tc, copied, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := readMacSelection(ctx, func(ctx context.Context) error { return ctx.Err() }, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
