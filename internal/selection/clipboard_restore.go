package selection

import (
	"errors"
	"strconv"
	"sync"
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
	sequence uint32
	formats  []clipboardFormat
}

type restoreOutcome uint8

const (
	restoreSkipped restoreOutcome = iota
	restoreComplete
	restoreFailed
)

// The caller holds the native clipboard open across the guard and all writes.
func restoreClipboardFormats(count, markers int, unchanged func() bool, clear func() error, write func(int) error) (restoreOutcome, error) {
	if !unchanged() {
		return restoreSkipped, nil
	}
	if err := clear(); err != nil {
		return restoreFailed, err
	}
	if err := writeClipboardFormats(count, markers, write); err != nil {
		return restoreFailed, err
	}
	return restoreComplete, nil
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

// At most one failed snapshot stays in memory; never write it in the background.
// New fallback copies are blocked until it is recovered or explicitly discarded.
var clipboardRecovery struct {
	sync.Mutex
	next     uint64
	id       string
	snapshot *clipboardSnapshot
	sequence uint32
}

func retainClipboardRecovery(snapshot *clipboardSnapshot, sequence uint32) {
	clipboardRecovery.Lock()
	defer clipboardRecovery.Unlock()
	clipboardRecovery.next++
	clipboardRecovery.id = strconv.FormatUint(clipboardRecovery.next, 10)
	clipboardRecovery.snapshot, clipboardRecovery.sequence = snapshot, sequence
}

func PendingClipboardRecovery() string {
	clipboardRecovery.Lock()
	defer clipboardRecovery.Unlock()
	return clipboardRecovery.id
}

func DiscardClipboardRecovery(id string) {
	<-clipboardMu
	defer func() { clipboardMu <- struct{}{} }()
	clipboardRecovery.Lock()
	defer clipboardRecovery.Unlock()
	if id == clipboardRecovery.id {
		clipboardRecovery.id, clipboardRecovery.snapshot = "", nil
	}
}
