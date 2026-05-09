package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	appdb "hackathon/apps/server/internal/db"
)

// TestCheckNoPlaintextMessages_FreshDB asserts the L18 boot guard
// returns nil against a freshly-opened DB whose tables don't exist
// yet. Apply later creates them; the guard runs before Apply, so
// "table absent" must read as "no plaintext to lose."
func TestCheckNoPlaintextMessagesFreshDB(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := appdb.Open(filepath.Join(dir, "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := checkNoPlaintextMessages(context.Background(), sqlDB); err != nil {
		t.Fatalf("guard: got %v want nil on fresh db", err)
	}
}

// TestCheckNoPlaintextMessages_PostMigration asserts the guard
// passes a DB that already carries the cipher_suite column —
// Apply has run; migration is idempotent; no plaintext is at risk.
func TestCheckNoPlaintextMessagesPostMigration(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := appdb.Open(filepath.Join(dir, "post.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := appdb.Apply(context.Background(), sqlDB); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := checkNoPlaintextMessages(context.Background(), sqlDB); err != nil {
		t.Fatalf("guard: got %v want nil on post-migration db", err)
	}
}

// TestCheckNoPlaintextMessages_PreEncryptionWithRows asserts the
// guard refuses a DB whose schema lacks cipher_suite AND already
// holds plaintext in messages. The error matches the L18 sentinel
// (and — by extension — its operator-facing wipe instructions for
// chat.db / chat.db-wal / chat.db-shm).
func TestCheckNoPlaintextMessagesPreEncryptionRefuses(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := appdb.Open(filepath.Join(dir, "pre.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	ctx := context.Background()
	// Re-create the pre-encryption schema for messages: 0006 adds
	// cipher_suite, so a table created without it stands in for an
	// older on-disk DB.
	if _, err := sqlDB.ExecContext(ctx, `CREATE TABLE messages (
	    id         TEXT PRIMARY KEY,
	    channel_id TEXT NOT NULL,
	    user_id    TEXT NOT NULL,
	    body       TEXT NOT NULL,
	    created_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create pre-encryption messages: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx,
		`INSERT INTO messages(id, channel_id, user_id, body, created_at)
		 VALUES ('m1', 'c1', 'u1', 'hi', '2026-05-09T00:00:00Z')`); err != nil {
		t.Fatalf("seed plaintext row: %v", err)
	}

	err = checkNoPlaintextMessages(ctx, sqlDB)
	if !errors.Is(err, errPreEncryptionDB) {
		t.Fatalf("guard: got %v want errPreEncryptionDB", err)
	}
	const wantSubstr = "rm chat.db chat.db-wal chat.db-shm"
	if err == nil || !contains(err.Error(), wantSubstr) {
		t.Fatalf("guard message missing %q wipe instruction: %v", wantSubstr, err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
