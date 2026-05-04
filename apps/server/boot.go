package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/repo"
)

// openAndMigrate opens the SQLite database at dbPath, applies pending
// migrations, and returns the connection plus a fresh repo bound to it.
// The caller owns the *sql.DB and must Close it on shutdown.
//
// Returns (nil, nil, nil) if dbPath is empty — the phase-0 boot path
// used by scripts/smoke.sh's no-DB modes runs without a SQLite file.
func openAndMigrate(dbPath string) (*sql.DB, *repo.Repo, error) {
	if dbPath == "" {
		return nil, nil, nil
	}
	sqlDB, err := appdb.Open(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("db open: %w", err)
	}
	migCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := appdb.Apply(migCtx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("db migrate: %w", err)
	}
	repository, err := repo.New(sqlDB)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("repo init: %w", err)
	}
	return sqlDB, repository, nil
}

// requireSecret reads env[name] and returns its bytes; errors if empty.
// Used for CHAT_JWT_SECRET, which is mandatory whenever CHAT_DB_PATH
// is set (the auth feature refuses to start without it).
func requireSecret(name, whenSet string) ([]byte, error) {
	v := os.Getenv(name)
	if v == "" {
		return nil, fmt.Errorf("config: %s must be set when %s is set", name, whenSet)
	}
	return []byte(v), nil
}
