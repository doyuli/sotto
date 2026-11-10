//go:build windows

package selection

import (
	"bytes"
	"testing"
)

// Exercise GDI handle cloning without replacing the user's clipboard.
func TestClipboardPaletteHandleRoundTrip(t *testing.T) {
	data := []byte{0, 3, 2, 0, 255, 0, 0, 0, 0, 128, 255, 0}
	h, err := prepareClipboardFormat(clipboardFormat{id: cfPalette, data: data})
	if err != nil {
		t.Fatal(err)
	}
	p := &preparedClipboard{formats: []clipboardFormat{{id: cfPalette, data: data}}, handles: []uintptr{h}}
	defer p.free()
	got, err := readClipboardPalette(h, clipboardOtherLimit)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("palette round trip: data=%v err=%v", got, err)
	}
	if _, err := readClipboardPalette(h, uintptr(len(data)-1)); err == nil {
		t.Fatal("palette read ignored allocation budget")
	}
	p.free()
	if p.handles[0] != 0 {
		t.Fatal("untransferred palette handle retained")
	}
	if _, err := readClipboardPalette(h, clipboardOtherLimit); err == nil {
		t.Fatal("palette GDI object was not deleted")
	}
}

func TestClipboardPalettePreparationRejectsInvalidPayload(t *testing.T) {
	for _, data := range [][]byte{nil, {0, 3}, {0, 3, 0, 0}, {0, 3, 2, 0, 1, 2, 3, 0}, {0, 0, 1, 0, 1, 2, 3, 0}} {
		if h, err := prepareClipboardFormat(clipboardFormat{id: cfPalette, data: data}); err == nil {
			procDeleteObject.Call(h)
			t.Fatalf("invalid palette accepted: %v", data)
		}
	}
}
