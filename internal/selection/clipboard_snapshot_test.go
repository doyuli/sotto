package selection

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func clipboardFormatsForTest(ids []uint16) func(uint16) (uint16, error) {
	i := 0
	return func(uint16) (uint16, error) {
		if i == len(ids) {
			return 0, nil
		}
		id := ids[i]
		i++
		return id, nil
	}
}

func TestClipboardSnapshotPreservesImageRepresentationsAndOtherFormats(t *testing.T) {
	for _, ids := range [][]uint16{
		{cfBitmap, cfDIB, cfDIBV5, cfPalette, 0xC001, 13},
		{cfDIBV5, cfBitmap, cfDIB, 0xC001, cfPalette, 13},
		{cfDIB, cfBitmap},
		{cfDIBV5, cfBitmap},
	} {
		snap, err := snapshotClipboardFormats(clipboardFormatsForTest(ids), func(id uint16, remaining uintptr) ([]byte, error) {
			if id == cfBitmap {
				t.Fatal("bitmap handle treated as global memory")
			}
			return []byte{byte(id), byte(id >> 8), 42}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var want []uint16
		for _, id := range ids {
			if id != cfBitmap {
				want = append(want, id)
			}
		}
		var got []uint16
		for _, f := range snap.formats {
			got = append(got, f.id)
			if !bytes.Equal(f.data, []byte{byte(f.id), byte(f.id >> 8), 42}) {
				t.Fatalf("format %d lost its original payload", f.id)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("formats=%v want=%v", got, want)
		}
	}
}

func TestClipboardSnapshotRequiresSavedDIBBeforeOmittingBitmap(t *testing.T) {
	failure := errors.New("delayed image unavailable")
	for _, ids := range [][]uint16{{cfBitmap}, {cfBitmap, 13}, {cfBitmap, cfDIB}, {cfDIBV5, cfBitmap}} {
		snap, err := snapshotClipboardFormats(clipboardFormatsForTest(ids), func(id uint16, _ uintptr) ([]byte, error) {
			if id == cfDIB || id == cfDIBV5 {
				return nil, failure
			}
			return []byte{1}, nil
		})
		if snap != nil || err == nil {
			t.Fatalf("unsafe image snapshot accepted: formats=%v snap=%v err=%v", ids, snap, err)
		}
		if len(ids) > 1 && ids[1] != 13 && !errors.Is(err, failure) {
			t.Fatalf("lost image read failure: %v", err)
		}
	}
}

func TestClipboardSnapshotDoesNotDropUnknownOrOwnerDependentFormats(t *testing.T) {
	for _, id := range []uint16{3, 14, 0x80, 0x82, 0x83, 0x8E, 0xC001} {
		failure := errors.New("private format cannot be locked")
		snap, err := snapshotClipboardFormats(clipboardFormatsForTest([]uint16{cfDIBV5, id}), func(id uint16, _ uintptr) ([]byte, error) {
			if id == cfDIBV5 {
				return []byte{1}, nil
			}
			if id != 0xC001 {
				t.Fatalf("unsupported handle format %d was read as HGLOBAL", id)
			}
			return nil, failure
		})
		if snap != nil || err == nil {
			t.Fatalf("silently dropped format %d", id)
		}
		if id == 0xC001 && !errors.Is(err, failure) {
			t.Fatalf("lost private format read failure: %v", err)
		}
	}
}

func TestClipboardSnapshotEnumerationFailureIsNotEmptyClipboard(t *testing.T) {
	failure := errors.New("enumeration failed")
	for _, after := range []uint16{0, cfDIB} {
		snap, err := snapshotClipboardFormats(func(previous uint16) (uint16, error) {
			if previous == after {
				return 0, failure
			}
			return after, nil
		}, func(uint16, uintptr) ([]byte, error) { return []byte{1}, nil })
		if snap != nil || !errors.Is(err, failure) {
			t.Fatalf("accepted incomplete enumeration: snap=%v err=%v", snap, err)
		}
	}
	snap, err := snapshotClipboardFormats(clipboardFormatsForTest(nil), func(uint16, uintptr) ([]byte, error) {
		t.Fatal("read from empty clipboard")
		return nil, nil
	})
	if err != nil || snap == nil || len(snap.formats) != 0 {
		t.Fatalf("empty clipboard refused: snap=%v err=%v", snap, err)
	}
}

func TestClipboardImageBudgetSupports4KAndBoundsTotalAllocation(t *testing.T) {
	var budget clipboardBudget
	// Both uncompressed representations of a 4K screenshot exceed the old
	// 32 MiB total. No large allocation is needed to exercise the policy.
	for _, id := range []uint16{cfDIB, cfDIBV5} {
		if err := budget.add(id, 3840*2160*4+124); err != nil {
			t.Fatal(err)
		}
	}
	if err := budget.add(0xC001, 8<<20); err != nil {
		t.Fatal("PNG/other payload should fit alongside a 4K image:", err)
	}
	remaining := budget.remaining(cfDIBV5)
	if err := budget.add(cfDIBV5, remaining); err != nil {
		t.Fatal(err)
	}
	before := budget
	for _, size := range []uintptr{1, ^uintptr(0)} {
		if err := budget.add(cfDIB, size); err == nil || budget != before {
			t.Fatal("exceeded or overflowed the image budget")
		}
	}
}

func TestClipboardImageDoesNotRelaxNonImageBudget(t *testing.T) {
	var budget clipboardBudget
	if err := budget.add(cfDIB, 1); err != nil {
		t.Fatal(err)
	}
	if err := budget.add(13, clipboardOtherLimit); err != nil {
		t.Fatal(err)
	}
	if budget.remaining(cfDIBV5) == 0 || budget.remaining(0xC001) != 0 {
		t.Fatal("image and non-image budget accounting mixed")
	}
	for _, id := range []uint16{13, 0xC001, cfPalette} {
		before := budget
		if err := budget.add(id, 1); err == nil || budget != before {
			t.Fatalf("image allowed format %d to exceed the non-image cap", id)
		}
	}
	var empty clipboardBudget
	if err := empty.add(13, 0); err == nil {
		t.Fatal("zero-sized payload accepted")
	}
}

func TestClipboardSnapshotPassesBudgetBeforeReadAndRejectsEmptyData(t *testing.T) {
	for _, id := range []uint16{cfDIB, cfDIBV5, 13, 0xC001} {
		snap, err := snapshotClipboardFormats(clipboardFormatsForTest([]uint16{id}), func(_ uint16, remaining uintptr) ([]byte, error) {
			want := uintptr(clipboardOtherLimit)
			if id == cfDIB || id == cfDIBV5 {
				want = clipboardSnapshotLimit
			}
			if remaining != want {
				t.Fatalf("format %d read budget=%d want=%d", id, remaining, want)
			}
			return nil, nil
		})
		if snap != nil || err == nil {
			t.Fatalf("empty format %d accepted", id)
		}
	}
}
