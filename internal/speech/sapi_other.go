//go:build !windows

package speech

func runThread(fn func())        { fn() }
func newEngine() (engine, error) { return nil, ErrUnavailable }
