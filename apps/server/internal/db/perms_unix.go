//go:build !windows

package db

import (
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
)

// EnsureFile creates path with mode 0600 if missing and chmods it to 0600 if
// it already exists. SQLite's own open does not let us set the create mode,
// and the process umask would otherwise widen a freshly created file in the
// window between open and chmod. We tighten the umask for the duration of
// the open so the kernel never creates the file with a wider mode, then
// chmod is the belt-and-braces tightening for any pre-existing file.
//
// The umask change is process-wide and not goroutine-safe; this function is
// expected to be called once at startup before other goroutines touch the
// filesystem.
func EnsureFile(path string) error {
	prev := syscall.Umask(0o077)
	defer syscall.Umask(prev)

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, FileMode) //nolint:gosec // G304: path is the configured DB location (CHAT_DB_PATH); operator-controlled by design.
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

// WarnDirMode logs a soft warning if path is a directory with bits set for
// group or other (mode & 0077 != 0). Per PRD §9 the SQLite parent directory
// should be 0700; os.MkdirAll only honors that mode on directories it
// creates, so a pre-existing wider directory survives EnsureFile silently.
//
// The warning is informational only: the server keeps running. A missing
// directory is not an error here either — Open's MkdirAll fields that case.
func WarnDirMode(path string) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		log.Printf("WARN: stat SQLite parent directory %q: %v", path, err)
		return
	}
	if !info.IsDir() {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		log.Printf("WARN: SQLite parent directory %q has loose mode %#o; recommended 0700", path, info.Mode().Perm())
	}
}
