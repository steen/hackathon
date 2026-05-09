package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"hackathon/apps/server/internal/auth"
	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/repo"
)

// errPreEncryptionDB is the boot-guard sentinel: a pre-encryption
// chat.db with plaintext rows. The message is quoted verbatim from
// decision-log L18 — the trailing period is part of the spec, hence
// the staticcheck ST1005 suppression on the assignment line.
//
//nolint:staticcheck // ST1005 — L18-specified terminal punctuation is intentional.
var errPreEncryptionDB = errors.New(
	"chat.db contains plaintext messages from a pre-encryption build. " +
		"Encryption is destructive — back up the file if you care, then " +
		"run 'rm chat.db chat.db-wal chat.db-shm' and restart.",
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
	// L18 — refuse to migrate a pre-encryption DB that still holds
	// plaintext messages: 0006_encryption.sql is wipe-and-reset, so
	// silently rolling forward would lose the body column's contents.
	// The check runs BEFORE Apply so the on-disk schema is still the
	// pre-migration one (cipher_suite columns absent ⇒ migration
	// 0006 hasn't been applied yet).
	if err := checkNoPlaintextMessages(migCtx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
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

// checkNoPlaintextMessages aborts startup when the on-disk schema is
// still the pre-encryption one (no cipher_suite column on messages or
// dm_messages) AND at least one message row exists. The intent is
// L18: the encryption migration drops body content; running it
// against a populated pre-encryption DB silently loses plaintext.
//
// Branches:
//   - cipher_suite already present on both tables ⇒ post-migration DB,
//     proceed.
//   - cipher_suite absent but row counts are zero ⇒ fresh DB or one that
//     never carried plaintext, proceed (the ALTER TABLE in 0006 will
//     add the columns when Apply runs).
//   - cipher_suite absent AND a count is non-zero ⇒ refuse; instruct
//     the operator to wipe all three SQLite WAL files (chat.db,
//     chat.db-wal, chat.db-shm) and restart.
//
// On a fresh install, the messages and dm_messages tables don't exist
// yet (0001/0005 haven't been applied); both branches resolve to
// "schema absent, count zero" and the function returns nil.
func checkNoPlaintextMessages(ctx context.Context, sqlDB *sql.DB) error {
	for _, table := range []string{"messages", "dm_messages"} {
		hasTable, err := tableExists(ctx, sqlDB, table)
		if err != nil {
			return fmt.Errorf("db: check %s table: %w", table, err)
		}
		if !hasTable {
			continue
		}
		hasCipherSuite, err := columnExists(ctx, sqlDB, table, "cipher_suite")
		if err != nil {
			return fmt.Errorf("db: check %s.cipher_suite: %w", table, err)
		}
		if hasCipherSuite {
			continue
		}
		var n int
		row := sqlDB.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)) //nolint:gosec // table name is from a static allow-list above.
		if err := row.Scan(&n); err != nil {
			return fmt.Errorf("db: count %s: %w", table, err)
		}
		if n > 0 {
			return errPreEncryptionDB
		}
	}
	return nil
}

func tableExists(ctx context.Context, sqlDB *sql.DB, table string) (bool, error) {
	row := sqlDB.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, table)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func columnExists(ctx context.Context, sqlDB *sql.DB, table, column string) (bool, error) {
	rows, err := sqlDB.QueryContext(ctx,
		fmt.Sprintf(`PRAGMA table_info(%s)`, table)) //nolint:gosec // table name is from a static allow-list above.
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
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
