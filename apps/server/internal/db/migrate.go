// Package db owns the SQLite handle lifecycle and the migration runner.
//
// The runner is intentionally tiny: it keeps a `schema_migrations` table of
// applied filenames and applies any pending file from the embedded migration
// set in lexicographic order, inside a transaction. We do not use goose here
// because (a) the migration set is small, (b) we want zero extra deps in the
// bootstrap path, and (c) the file naming convention is goose-compatible so
// we can swap implementations later without renaming files.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"hackathon/migrations"
)

// Apply runs every pending migration from the project's embedded set. Safe to
// call repeatedly: previously-applied migrations are skipped.
func Apply(ctx context.Context, sqlDB *sql.DB) error {
	return ApplyFS(ctx, sqlDB, migrations.FS)
}

// ApplyFS is Apply with an injectable migration source. Each `*.sql` entry at
// the root of fsys is treated as one migration; the entry name is the key
// recorded in schema_migrations.
func ApplyFS(ctx context.Context, sqlDB *sql.DB, fsys fs.FS) error {
	if sqlDB == nil {
		return errors.New("db.ApplyFS: nil *sql.DB")
	}
	if fsys == nil {
		return errors.New("db.ApplyFS: nil fs.FS")
	}
	if err := ensureMigrationsTable(ctx, sqlDB); err != nil {
		return err
	}
	applied, err := loadApplied(ctx, sqlDB)
	if err != nil {
		return err
	}
	files, err := listMigrationFiles(fsys)
	if err != nil {
		return err
	}
	for _, name := range files {
		if applied[name] {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("db.ApplyFS: read %q: %w", name, err)
		}
		if err := applyOne(ctx, sqlDB, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func ensureMigrationsTable(ctx context.Context, sqlDB *sql.DB) error {
	const ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
    name       TEXT PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
)`
	if _, err := sqlDB.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}
	return nil
}

func loadApplied(ctx context.Context, sqlDB *sql.DB) (map[string]bool, error) {
	rows, err := sqlDB.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("db: select schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("db: scan schema_migrations row: %w", err)
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: iterate schema_migrations: %w", err)
	}
	return applied, nil
}

func listMigrationFiles(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func applyOne(ctx context.Context, sqlDB *sql.DB, name, body string) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx for %q: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("db: apply %q: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(name) VALUES (?)`, name); err != nil {
		return fmt.Errorf("db: record %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit %q: %w", name, err)
	}
	return nil
}
