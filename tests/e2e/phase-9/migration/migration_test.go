// Package migration_e2e_test is a black-box check that the production
// chat-server binary, started against a fresh tempdir SQLite file,
// applies migration 0005 and leaves the four phase-9 tables in place
// plus the two new channels columns. We boot via the shared
// `testsupport.StartServer` harness (decision-log L27) so any future
// changes to the migration runner or the binary's startup sequence
// flow through one helper.
//
// The test opens the on-disk SQLite file read-only after the server
// has finished migrating; it does not import any apps/** internal
// package. That keeps the assertion at the wire boundary (the schema
// the running server actually leaves on disk).
package migration_e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"testing"

	_ "modernc.org/sqlite"

	"hackathon/tests/e2e/internal/testsupport"
)

// TestMigration0005AppliesPhase9Schema asserts the four new tables
// (dm_conversations, dm_messages, channel_reads, dm_reads) and the
// two new channels columns (last_message_id, last_message_at) are
// present after the server boots against an empty DB. Issue #862 AC.
func TestMigration0005AppliesPhase9Schema(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	db := openSchemaDB(t, srv.DBPath)
	ctx := context.Background()

	for _, table := range []string{
		"dm_conversations",
		"dm_messages",
		"channel_reads",
		"dm_reads",
	} {
		assertTableExists(ctx, t, db, table)
	}

	assertColumnExists(ctx, t, db, "channels", "last_message_id")
	assertColumnExists(ctx, t, db, "channels", "last_message_at")
}

// TestMigration0005CreatesIndexes pins the indexes the listing queries
// will rely on (decision-log L13). A future migration that renames or
// drops one will silently regress the listing access path; this test
// catches that.
func TestMigration0005CreatesIndexes(t *testing.T) {
	srv := testsupport.StartServer(t, testsupport.StartOptions{})
	db := openSchemaDB(t, srv.DBPath)
	ctx := context.Background()

	for _, idx := range []string{
		"idx_dm_messages_conv_created",
		"idx_channel_reads_user",
		"idx_channels_last_message_at",
	} {
		assertIndexExists(ctx, t, db, idx)
	}
}

// openSchemaDB opens the running server's SQLite file read-only so
// SELECTs do not contend with the server's writes. mode=ro avoids any
// chance of the test creating the file if the path is wrong (it would
// error instead). Cleanup via t.Cleanup so callers do not need to
// Close().
func openSchemaDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)",
		(&url.URL{Path: dbPath}).EscapedPath())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite ro: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertTableExists(ctx context.Context, t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var got string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&got)
	if err != nil {
		t.Errorf("table %q: %v", name, err)
		return
	}
	if got != name {
		t.Errorf("table %q: got name=%q", name, got)
	}
}

func assertIndexExists(ctx context.Context, t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var got string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name,
	).Scan(&got)
	if err != nil {
		t.Errorf("index %q: %v", name, err)
		return
	}
	if got != name {
		t.Errorf("index %q: got name=%q", name, got)
	}
}

// assertColumnExists scans `PRAGMA table_info(<table>)` and fails if
// no row matches `colName`. PRAGMA queries do not accept bound
// parameters for the table name, so the table is interpolated; values
// here are test-controlled constants (no untrusted input).
func assertColumnExists(ctx context.Context, t *testing.T, db *sql.DB, table, colName string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer func() { _ = rows.Close() }()
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
			t.Fatalf("scan pragma row for %s: %v", table, err)
		}
		if cname == colName {
			if err := rows.Err(); err != nil {
				t.Fatalf("iter pragma %s: %v", table, err)
			}
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iter pragma %s: %v", table, err)
	}
	t.Errorf("column %s.%s missing", table, colName)
}
