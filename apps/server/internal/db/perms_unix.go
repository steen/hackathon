//go:build !windows

package db

import (
	"fmt"
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
