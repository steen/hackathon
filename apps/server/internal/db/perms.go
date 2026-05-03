// Package db provides path-level helpers around the on-disk SQLite database
// file. It deliberately avoids importing a SQL driver so it can be reused by
// whichever package owns the *sql.DB lifecycle.
//
// EnsureFile lives in perms_unix.go (POSIX hosts) and perms_windows.go
// (Windows local dev). The unix variant is the production path that honors
// SEC-14; the Windows variant is a no-op stub so the package compiles for
// local development.
package db

import "os"

// FileMode is the on-disk permission required for the SQLite database file
// per PRD §9 / SEC-14: owner read/write only.
const FileMode os.FileMode = 0o600
