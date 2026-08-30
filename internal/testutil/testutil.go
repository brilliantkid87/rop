// Package testutil provides small helpers shared by ROP tests.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// RepoRoot returns the absolute path of the repository root, derived from
// this file's location, so tests are independent of the working directory.
func RepoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testutil: cannot determine repo root")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// TempDirForDB returns a directory for a test database and registers
// deterministic cleanup. On Windows, modernc.org/sqlite WAL files can outlive
// Close() by a few milliseconds; we close, then remove with a short retry so
// t.TempDir's RemoveAll does not race the handle release.
func TempDirForDB(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ropdb-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		time.Sleep(20 * time.Millisecond)
		for i := 0; i < 10; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	return dir
}
