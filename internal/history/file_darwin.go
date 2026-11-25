package history

import "os"

func prepareHistoryFile(path string) error {
	// SQLite otherwise creates a readable file in an already-existing user data directory.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}
