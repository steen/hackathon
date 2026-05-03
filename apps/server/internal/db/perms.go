// Package db provides path-level helpers around the on-disk SQLite database
// file. It deliberately avoids importing a SQL driver so it can be reused by
// whichever package owns the *sql.DB lifecycle.
package db

import (
	"fmt"
	"os"
)

// FileMode is the on-disk permission required for the SQLite database file
// per PRD §9 / SEC-14: owner read/write only.
const FileMode os.FileMode = 0o600

// EnsureFile creates path with mode 0600 if missing and chmods it to 0600 if
// it already exists. SQLite's own open does not let us set the create mode,
// and the process umask can widen a freshly created file, so we either
// pre-create with the strict mode or tighten an existing file in place.
func EnsureFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, FileMode)
	if err != nil {
		return fmt.Errorf("db: open %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("db: close %q: %w", path, err)
	}
	if err := os.Chmod(path, FileMode); err != nil {
		return fmt.Errorf("db: chmod %q: %w", path, err)
	}
	return nil
}
