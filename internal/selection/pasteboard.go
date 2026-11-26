package selection

import "fmt"

type pasteboardFormat struct {
	name string
	data []byte
}
type pasteboardItem []pasteboardFormat
type pasteboardRestoreOps struct {
	prepare func() error
	allowed func() bool
	change  func() int64
	clear   func() int64
	write   func() error
}

func restorePasteboardContents(expected int64, ops pasteboardRestoreOps) error {
	if err := ops.prepare(); err != nil {
		return err
	}
	if !ops.allowed() || ops.change() != expected {
		return ErrSourceChanged
	}
	cleared := ops.clear()
	if ops.change() != cleared {
		return ErrSourceChanged
	}
	return ops.write()
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
