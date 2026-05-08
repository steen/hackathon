package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"hackathon/apps/server/internal/auth"
	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/repo"
)

// openAndMigrate opens the SQLite database at dbPath, applies pending
// migrations, and returns the connection plus a fresh repo bound to it.
// The caller owns the *sql.DB and must Close it on shutdown.
//
// CHAT_DB_PATH is required at startup (config.Validate enforces it
// before this function is called); an empty path here is a programmer
// error and surfaces as the underlying db.Open failure.
func openAndMigrate(dbPath string) (*sql.DB, *repo.Repo, error) {
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

// applyBcryptCost installs the bcrypt cost on the auth package. The
// value is the one cfg.Validate parsed from CHAT_BCRYPT_COST and stored
// on Config; Validate already enforced the [MinBcryptCost, MaxBcryptCost]
// range, so SetBcryptCost's re-check here is defensive. Must run before
// any goroutine that calls auth.Hash; main.go invokes this between
// cfg.Validate() and the HTTP listener start so the boot path aborts
// on an unexpected error before opening a port.
func applyBcryptCost(cost int) error {
	return auth.SetBcryptCost(cost)
}
