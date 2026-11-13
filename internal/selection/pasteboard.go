package selection

import "fmt"

type pasteboardFormat struct {
	name string
	data []byte
}
type pasteboardItem []pasteboardFormat
type macClipboardSnapshot struct {
	items                 []pasteboardItem
	change, restoreChange int64
}

type pasteboardRestoreOps struct {
	prepare func() error
	allowed func() bool
	change  func() int64
	clear   func() int64
	write   func() error
}

// Return only a version already verified by the caller or produced by our own
// clear. A later read may belong to another writer, even when a write failed.
func restorePasteboardContents(expected int64, ops pasteboardRestoreOps) (restored bool, retryChange int64, err error) {
	retryChange = expected
	if expected < 0 {
		return false, -1, ErrSourceChanged
	}
	if err = ops.prepare(); err != nil {
		return
	}
	if !ops.allowed() || ops.change() != expected {
		return false, -1, ErrSourceChanged
	}
	retryChange = ops.clear()
	if ops.change() != retryChange {
		return false, -1, ErrSourceChanged
	}
	if err = ops.write(); err != nil {
		return
	}
	return true, retryChange, nil
}

// Eagerly materialize every representation before copying; a file promise or
// unreadable private type must never cause a lossy text-only restoration.
func snapshotPasteboard(types [][]string, read func(int, string, int) ([]byte, error)) ([]pasteboardItem, error) {
	if len(types) > 1024 {
		return nil, fmt.Errorf("too many pasteboard items")
	}
	var total, other int
	items := make([]pasteboardItem, len(types))
	for i, names := range types {
		if len(names) == 0 || len(names) > 256 {
			return nil, fmt.Errorf("invalid pasteboard representations")
		}
		for _, name := range names {
			remaining := clipboardSnapshotLimit - total
			image := name == "public.png" || name == "public.tiff"
			if !image {
				remaining = min(remaining, clipboardOtherLimit-other)
			}
			data, err := read(i, name, remaining)
			if err != nil {
				return nil, fmt.Errorf("materialize pasteboard item %d type %s: %w", i, name, err)
			}
			if len(data) > remaining {
				return nil, fmt.Errorf("pasteboard snapshot exceeds budget")
			}
			total += len(data)
			if !image {
				other += len(data)
			}
			items[i] = append(items[i], pasteboardFormat{name, data})
		}
	}
	return items, nil
}
