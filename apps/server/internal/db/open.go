package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // SQL driver registration; used by sql.Open("sqlite", ...).
)

// Open returns a *sql.DB pointing at a SQLite file at path. EnsureFile (SEC-14)
// creates the file at 0600 if missing and tightens an existing too-wide file —
// this is the single canonical entry point for "the SQLite file is on disk
// with the right permissions" so the driver never opens a world-readable file.
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
	if err := EnsureFile(path); err != nil {
		return nil, fmt.Errorf("db.Open: %w", err)
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
