package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open returns a *sql.DB pointing at a SQLite file at path. The file is
// created with mode 0600 if missing (PRD §9 — the DB stores password hashes
// and audit data; never world-readable). Existing files are not chmodded; a
// warning belongs in the startup checks feature, not here.
//
// The driver name is "sqlite" (modernc.org/sqlite), not "sqlite3".
func Open(path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("db.Open: empty path")
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("db.Open: mkdir %q: %w", dir, err)
		}
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// Create the file with 0600 before the driver opens it; modernc/sqlite
		// will respect existing perms and not widen them.
		f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if ferr != nil {
			return nil, fmt.Errorf("db.Open: pre-create %q: %w", path, ferr)
		}
		_ = f.Close()
	} else if err != nil {
		return nil, fmt.Errorf("db.Open: stat %q: %w", path, err)
	}

	// _pragma options: WAL is friendlier to concurrent readers + a single
	// writer (the only shape we have); foreign_keys=ON enforces our REFERENCES.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db.Open: sql.Open: %w", err)
	}
	// SetMaxOpenConns(1) serializes all access at the pool layer. PRD §14
	// commits to "single-process server with serialized writes" at friend
	// scale; this matches that and removes any "database is locked" risk
	// from concurrent writers. Revisit if read throughput becomes a concern.
	sqlDB.SetMaxOpenConns(1)
	return sqlDB, nil
}
