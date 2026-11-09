//go:build !windows

package ocr

func cachedOCRResources() engineResources   { return engineResources{} }
func releaseIdleResource(int) (bool, error) { return true, nil }
