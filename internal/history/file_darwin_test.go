package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryPrivateDatabase(t *testing.T) {
	// The parent may already exist with wider permissions when SQLite opens the file.
	dir := t.TempDir()
	s, err := Open(t.Context(), dir, Policy{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, Filename))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("new database mode = %o, want 0600", mode)
	}
}
