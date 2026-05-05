//go:build windows

package db

import (
	"fmt"
	"os"
)

// EnsureFile is a no-op on Windows: NTFS does not have POSIX file modes, so
// the SEC-14 "owner-only" guarantee cannot be expressed via os.Chmod and the
// umask trick used in the unix variant has no analogue. The file is created
// if missing using os.OpenFile so callers see the same "file exists"
// post-condition as on POSIX hosts. Production deployments are unix-only;
// this branch exists so local development on Windows can build the package.
func EnsureFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, FileMode) //nolint:gosec // G304: path is the configured DB location (CHAT_DB_PATH); operator-controlled by design.
	if err != nil {
		return fmt.Errorf("db: open %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("db: close %q: %w", path, err)
	}
	return nil
}

// WarnDirMode is a no-op on Windows: NTFS does not have POSIX mode bits,
// so the "0700 recommended" check from PRD §9 does not apply. The symbol
// exists so callers do not need a build tag at the call site.
func WarnDirMode(path string) {
	_ = path
}
