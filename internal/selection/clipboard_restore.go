package selection

import (
	"context"
	"errors"
	"fmt"
	"log"
)

var clipboardMu = make(chan struct{}, 1)

func init() { clipboardMu <- struct{}{} }

type clipboardFormat struct {
	id uint16
	// CF_PALETTE stores a LOGPALETTE description; other supported formats
	// store the bytes of their global-memory object.
	data []byte
}

type clipboardSnapshot struct {
	formats []clipboardFormat
}

// The caller holds the native clipboard open across the guard and all writes.
func restoreClipboardFormats(count, markers int, unchanged func() bool, clear func() error, write func(int) error) error {
	if !unchanged() {
		return ErrSourceChanged
	}
	if err := clear(); err != nil {
		return err
	}
	return writeClipboardFormats(count, markers, write)
}

// Retry only the failing format, while still holding the clipboard open.
// Never clear successful writes again. Payloads require all protection markers.
func writeClipboardFormats(count, markers int, write func(int) error) error {
	var failures []error
	for i := 0; i < count; i++ {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = write(i); err == nil {
				break
			}
		}
		if err != nil {
			failures = append(failures, err)
			if i < markers {
				break
			}
		}
	}
	return errors.Join(failures...)
}

// A failed write-back does not invalidate text already read from a stable copy.
// Even an empty selection must run restoration if the copy replaced the clipboard.
func finishClipboardCopy(ctx context.Context, text string, restoreErr error) (Selection, error) {
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	if errors.Is(restoreErr, ErrSourceChanged) {
		return Selection{}, restoreErr
	}
	if restoreErr != nil {
		if text == "" {
			return Selection{}, fmt.Errorf("restore clipboard: %w", restoreErr)
		}
		log.Printf("selection: clipboard restore incomplete: %v", restoreErr)
	}
	if text == "" {
		return Selection{}, ErrNoSelection
	}
	return Selection{Text: text, Via: "clipboard"}, nil
}
