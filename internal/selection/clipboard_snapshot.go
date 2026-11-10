package selection

import "fmt"

const (
	cfBitmap  = 2
	cfDIB     = 8
	cfPalette = 9
	cfDIBV5   = 17

	// A 4K image can advertise both DIB and DIBV5 (about 64 MiB together).
	// Keep both representations: they can carry different alpha/color metadata.
	// Restore preallocation uses up to one additional copy of this budget.
	clipboardSnapshotLimit = 128 << 20
	clipboardOtherLimit    = 32 << 20
)

// The budget is checked before reading/allocating a payload. A small image
// must not let an unrelated private format consume the larger image budget.
type clipboardBudget struct {
	total uintptr
	other uintptr
}

func (b clipboardBudget) remaining(id uint16) uintptr {
	remaining := uintptr(clipboardSnapshotLimit) - b.total
	if id != cfDIB && id != cfDIBV5 {
		remaining = min(remaining, uintptr(clipboardOtherLimit)-b.other)
	}
	return remaining
}

func (b *clipboardBudget) add(id uint16, size uintptr) error {
	if size == 0 {
		return fmt.Errorf("clipboard format %d has no preservable data", id)
	}
	if remaining := b.remaining(id); size > remaining {
		return fmt.Errorf("clipboard format %d exceeds snapshot budget: bytes=%d remaining=%d", id, size, remaining)
	}
	b.total += size
	if id != cfDIB && id != cfDIBV5 {
		b.other += size
	}
	return nil
}

// Enumeration and reads stay interleaved under one clipboard open: delayed
// rendering may materialize more formats. Only CF_BITMAP is replaceable, and
// only after a DIB representation has actually been saved. All other read
// failures abort before simulated copy, including unknown registered formats.
func snapshotClipboardFormats(next func(uint16) (uint16, error), read func(uint16, uintptr) ([]byte, error)) (*clipboardSnapshot, error) {
	var snap clipboardSnapshot
	var budget clipboardBudget
	var bitmap, dib bool
	for previous := uint16(0); ; {
		id, err := next(previous)
		if err != nil {
			return nil, fmt.Errorf("enumerate clipboard after format %d: %w", previous, err)
		}
		if id == 0 {
			break
		}
		previous = id
		switch id {
		case cfBitmap:
			bitmap = true
			continue
		case 3, 14, 0x80, 0x82, 0x83, 0x8E:
			// Metafiles and owner/display formats need their own lifetime
			// handling; copying their outer memory block is not sufficient.
			return nil, fmt.Errorf("clipboard format %d requires unsupported handle/owner preservation", id)
		}
		data, err := read(id, budget.remaining(id))
		if err != nil {
			return nil, fmt.Errorf("read clipboard format %d: %w", id, err)
		}
		if err := budget.add(id, uintptr(len(data))); err != nil {
			return nil, err
		}
		snap.formats = append(snap.formats, clipboardFormat{id: id, data: data})
		dib = dib || id == cfDIB || id == cfDIBV5
	}
	if bitmap && !dib {
		return nil, fmt.Errorf("clipboard bitmap has no saved DIB or DIBV5 representation")
	}
	return &snap, nil
}
