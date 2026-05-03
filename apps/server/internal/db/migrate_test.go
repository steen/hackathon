package db_test

import (
	"context"
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/migrations"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	// ":memory:" (not "file::memory:?cache=shared") gives a fresh DB per
	// sql.Open call; tests must not share state.
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

// TestApplyAppliesEmbeddedMigrations verifies the production migration set
// runs cleanly against a fresh in-memory DB and leaves the expected tables.
// (Acceptance: `migrations/0001_init.sql` defines users/channels/messages/auth_events.)
func TestApplyAppliesEmbeddedMigrations(t *testing.T) {
	ctx := context.Background()
	sqlDB := openMemDB(t)

	if err := appdb.Apply(ctx, sqlDB); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, table := range []string{"users", "channels", "messages", "auth_events", "schema_migrations"} {
		var name string
		err := sqlDB.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist after Apply: %v", table, err)
		}
	}

	// users.token_version must exist as a column (US-12 scaffolding).
	rows, err := sqlDB.QueryContext(ctx, `PRAGMA table_info(users)`)
	if err != nil {
		t.Fatalf("pragma table_info(users): %v", err)
	}
	defer rows.Close()
	foundTV := false
	for rows.Next() {
		var (
			cid     int
			cname   string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		if cname == "token_version" {
			foundTV = true
		}
	}
	if !foundTV {
		t.Error("users.token_version column missing")
	}
}

// TestApplyIsIdempotent re-runs Apply on an already-migrated DB and verifies
// no error and no duplicate rows in schema_migrations.
// (Acceptance: "migration runner ... is idempotent on re-run".)
func TestApplyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	sqlDB := openMemDB(t)

	if err := appdb.Apply(ctx, sqlDB); err != nil {
		t.Fatalf("Apply (first): %v", err)
	}
	if err := appdb.Apply(ctx, sqlDB); err != nil {
		t.Fatalf("Apply (second): %v", err)
	}

	var n int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	// Exactly one migration in the set today; if more land, this asserts
	// "no duplicates" by matching the file count discovered at runtime.
	want := countMigrationFiles(t)
	if n != want {
		t.Errorf("schema_migrations row count = %d; want %d", n, want)
	}
}

// TestApplyFSAppliesPendingOnly verifies that adding a new migration after a
// previous Apply call only applies the new file, leaving existing data intact.
func TestApplyFSAppliesPendingOnly(t *testing.T) {
	ctx := context.Background()
	sqlDB := openMemDB(t)

	first := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE t1(id INTEGER PRIMARY KEY); INSERT INTO t1(id) VALUES (1);`),
		},
	}
	if err := appdb.ApplyFS(ctx, sqlDB, first); err != nil {
		t.Fatalf("ApplyFS first: %v", err)
	}

	second := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			// Same file body as before; runner must skip it (else INSERT errors on PK).
			Data: []byte(`CREATE TABLE t1(id INTEGER PRIMARY KEY); INSERT INTO t1(id) VALUES (1);`),
		},
		"0002_extra.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE t2(id INTEGER PRIMARY KEY);`),
		},
	}
	if err := appdb.ApplyFS(ctx, sqlDB, second); err != nil {
		t.Fatalf("ApplyFS second: %v", err)
	}

	var t2 string
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='t2'`).Scan(&t2); err != nil {
		t.Errorf("expected t2 table after second ApplyFS: %v", err)
	}
}

func TestApplyRejectsNilDB(t *testing.T) {
	if err := appdb.ApplyFS(context.Background(), nil, fstest.MapFS{}); err == nil {
		t.Fatal("expected error for nil *sql.DB")
	}
}

func countMigrationFiles(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			n++
		}
	}
	return n
}
