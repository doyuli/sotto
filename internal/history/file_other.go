//go:build !darwin

package history

// Windows databases inherit the user profile directory's ACL.
func prepareHistoryFile(string) error { return nil }
