//go:build windows

package selection

import (
	"encoding/binary"
	"fmt"
	"log"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modGdi32                   = windows.NewLazySystemDLL("gdi32.dll")
	procGetObjectW             = modGdi32.NewProc("GetObjectW")
	procGetPaletteEntries      = modGdi32.NewProc("GetPaletteEntries")
	procCreatePalette          = modGdi32.NewProc("CreatePalette")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procGetClipboardFormatName = modUser32.NewProc("GetClipboardFormatNameW")
	procSetLastError           = modKernel32.NewProc("SetLastError")
)

// The caller pins the OS thread. Read delayed formats and record the final
// sequence under one open, before copy can replace any original content.
func saveClipboard() (snap *clipboardSnapshot, err error) {
	started := time.Now()
	defer func() {
		var formats, size int
		if snap != nil {
			formats = len(snap.formats)
			for _, f := range snap.formats {
				size += len(f.data)
			}
		}
		log.Printf("selection: clipboard stage=snapshot formats=%d bytes=%d elapsed=%s err=%v", formats, size, time.Since(started).Round(time.Millisecond), err)
	}()
	if !openClipboard() {
		return nil, fmt.Errorf("open clipboard for snapshot: clipboard remained unavailable")
	}
	defer closeClipboard()
	snap, err = snapshotClipboardFormats(func(previous uint16) (uint16, error) {
		// Zero means either end-of-enumeration or failure. Clear the last
		// error on this pinned thread so a stale error cannot mask the end.
		procSetLastError.Call(0)
		id, _, callErr := procEnumClipboardFormats.Call(uintptr(previous))
		if id == 0 && callErr != syscall.Errno(0) {
			return 0, callErr
		}
		if id != 0 {
			log.Printf("selection: clipboard stage=enumerate format=%d name=%q", id, clipboardFormatName(uint16(id)))
		}
		return uint16(id), nil
	}, readClipboardFormat)
	if err != nil {
		return nil, err
	}
	snap.sequence = clipboardSeq()
	return snap, nil
}

func clipboardFormatName(id uint16) string {
	switch id {
	case 1:
		return "CF_TEXT"
	case cfBitmap:
		return "CF_BITMAP"
	case 3:
		return "CF_METAFILEPICT"
	case 7:
		return "CF_OEMTEXT"
	case cfDIB:
		return "CF_DIB"
	case cfPalette:
		return "CF_PALETTE"
	case cfUnicodeText:
		return "CF_UNICODETEXT"
	case 14:
		return "CF_ENHMETAFILE"
	case 15:
		return "CF_HDROP"
	case 16:
		return "CF_LOCALE"
	case cfDIBV5:
		return "CF_DIBV5"
	case 0x80:
		return "CF_OWNERDISPLAY"
	case 0x82:
		return "CF_DSPBITMAP"
	case 0x83:
		return "CF_DSPMETAFILEPICT"
	case 0x8E:
		return "CF_DSPENHMETAFILE"
	}
	if id >= 0xC000 {
		var name [256]uint16
		n, _, _ := procGetClipboardFormatName.Call(uintptr(id), uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
		if n != 0 {
			return syscall.UTF16ToString(name[:n])
		}
	}
	return "unknown"
}

func readClipboardFormat(id uint16, remaining uintptr) (data []byte, err error) {
	started := time.Now()
	defer func() {
		log.Printf("selection: clipboard stage=read format=%d name=%q bytes=%d elapsed=%s err=%v", id, clipboardFormatName(id), len(data), time.Since(started).Round(time.Millisecond), err)
	}()
	h, _, err := procGetClipboardData.Call(uintptr(id))
	if h == 0 {
		return nil, clipboardCallError("GetClipboardData returned no handle", err)
	}
	if id == cfPalette {
		return readClipboardPalette(h, remaining)
	}
	size, _, err := procGlobalSize.Call(h)
	if size == 0 {
		return nil, clipboardCallError("GlobalSize returned zero", err)
	}
	if size > remaining {
		return nil, fmt.Errorf("snapshot budget exceeded: bytes=%d remaining=%d", size, remaining)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		return nil, clipboardCallError("GlobalLock returned no address", err)
	}
	defer procGlobalUnlock.Call(h)
	data = make([]byte, size)
	copy(data, unsafe.Slice((*byte)(native(p)), size))
	return data, nil
}

// A palette is a GDI object, not HGLOBAL. Store a LOGPALETTE description so
// restore/recovery can create a fresh handle without depending on its owner.
func readClipboardPalette(h, remaining uintptr) ([]byte, error) {
	var count uint16
	n, _, _ := procGetObjectW.Call(h, unsafe.Sizeof(count), uintptr(unsafe.Pointer(&count)))
	if n != unsafe.Sizeof(count) || count == 0 {
		return nil, fmt.Errorf("GetObjectW palette entry count unavailable")
	}
	size := 4 + uintptr(count)*4
	if size > remaining {
		return nil, fmt.Errorf("palette snapshot budget exceeded: bytes=%d remaining=%d", size, remaining)
	}
	data := make([]byte, size)
	binary.LittleEndian.PutUint16(data, 0x0300)
	binary.LittleEndian.PutUint16(data[2:], count)
	n, _, _ = procGetPaletteEntries.Call(h, 0, uintptr(count), uintptr(unsafe.Pointer(&data[4])))
	if n != uintptr(count) {
		return nil, fmt.Errorf("GetPaletteEntries incomplete: entries=%d expected=%d", n, count)
	}
	return data, nil
}

func prepareClipboardFormat(f clipboardFormat) (uintptr, error) {
	if len(f.data) == 0 {
		return 0, fmt.Errorf("empty clipboard payload")
	}
	if f.id == cfPalette {
		if len(f.data) < 8 || binary.LittleEndian.Uint16(f.data) != 0x0300 || 4+int(binary.LittleEndian.Uint16(f.data[2:]))*4 != len(f.data) {
			return 0, fmt.Errorf("invalid LOGPALETTE snapshot")
		}
		h, _, _ := procCreatePalette.Call(uintptr(unsafe.Pointer(&f.data[0])))
		if h == 0 {
			return 0, fmt.Errorf("CreatePalette failed")
		}
		return h, nil
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(f.data)))
	if h == 0 {
		return 0, clipboardCallError("GlobalAlloc returned no handle", err)
	}
	address, _, err := procGlobalLock.Call(h)
	if address == 0 {
		procGlobalFree.Call(h)
		return 0, clipboardCallError("GlobalLock returned no address", err)
	}
	copy(unsafe.Slice((*byte)(native(address)), len(f.data)), f.data)
	procGlobalUnlock.Call(h)
	return h, nil
}

// Some failed handle reads do not supply a last-error value. Do not append
// "The operation completed successfully" to a genuine preservation failure.
func clipboardCallError(operation string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s (no Win32 error reported)", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
