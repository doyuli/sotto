//go:build windows

package ocr

// Both operations require admission. The worker lock is nested inside it,
// never the other way around; metadata locks are not held here.
func cachedOCRResources() engineResources {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engineResources{ppEngine != nil, engine.cmd != nil}
}

func releaseIdleResource(id int) (bool, error) {
	if id == localOCR {
		r := ppEngine
		ppEngine = nil
		if r != nil {
			return true, r.Close()
		}
		return true, nil
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
