//go:build windows

package ocr

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Both operations require admission. The worker lock is nested inside it,
// never the other way around; metadata locks are not held here.
func cachedOCRResources() engineResources {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engineResources{ppEngine != nil || ortRT != nil, engine.cmd != nil}
}

func releaseIdleResource(id int) (bool, error) {
	if id == localOCR {
		r := ppEngine
		ppEngine = nil
		var err error
		if r != nil {
			err = r.Close()
		}
		unloadErr := unloadORT(windows.FreeLibrary)
		return unloadErr == nil, errors.Join(err, unloadErr)
	}
	return engine.releaseIdle(), nil
}

func (w *winrt) releaseIdle() bool {
	if !w.mu.TryLock() {
		return false
	}
	defer w.mu.Unlock()
	// admission kept the deadline valid while taking mu. Holding mu now
	// keeps the worker identity stable until killLocked has waited for exit.
	w.killLocked()
	return true
}
