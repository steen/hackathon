package seed_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
	"hackathon/apps/server/internal/seed"
	"hackathon/migrations"
)

func newRepo(t *testing.T) (*repo.Repo, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := appdb.Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := appdb.ApplyFS(context.Background(), sqlDB, migrations.FS); err != nil {
		t.Fatalf("ApplyFS: %v", err)
	}
	r, err := repo.New(sqlDB)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return r, sqlDB
}

func countChannelsNamedGeneral(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM channels WHERE name = ?`, "general").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// AC: on first boot with no channels, a channel named "general" is created.
func TestSeedCreatesGeneralWhenAbsent(t *testing.T) {
	r, db := newRepo(t)

	if err := seed.EnsureGeneralChannel(context.Background(), r); err != nil {
		t.Fatalf("EnsureGeneralChannel: %v", err)
	}

	if got := countChannelsNamedGeneral(t, db); got != 1 {
		t.Fatalf("rows: got %d want 1", got)
	}
}

// AC: re-running the server does not duplicate or error on the UNIQUE
// constraint when "general" already exists.
func TestSeedIsIdempotent(t *testing.T) {
	r, db := newRepo(t)

	for i := 0; i < 3; i++ {
		if err := seed.EnsureGeneralChannel(context.Background(), r); err != nil {
			t.Fatalf("EnsureGeneralChannel iter %d: %v", i, err)
		}
	}

	if got := countChannelsNamedGeneral(t, db); got != 1 {
		t.Fatalf("rows after 3 calls: got %d want 1", got)
	}
}

// If a channel named "general" was already created out-of-band (e.g. by an
// admin via the API), the seeder must not insert a second row, must not
// touch the existing row's id/created_at, and must not error.
func TestSeedRespectsPreexistingGeneralChannel(t *testing.T) {
	r, db := newRepo(t)

	preID := ids.NewULID()
	preCreated := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := r.CreateChannel(context.Background(), preID, "general", true, preCreated); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := seed.EnsureGeneralChannel(context.Background(), r); err != nil {
		t.Fatalf("EnsureGeneralChannel: %v", err)
	}

	if got := countChannelsNamedGeneral(t, db); got != 1 {
		t.Fatalf("rows: got %d want 1", got)
	}

	var gotID string
	if err := db.QueryRow(`SELECT id FROM channels WHERE name = ?`, "general").Scan(&gotID); err != nil {
		t.Fatalf("read id: %v", err)
	}
	if gotID != preID {
		t.Fatalf("seeder replaced row: got id %q want %q", gotID, preID)
	}
}

func TestSeedRejectsNilRepo(t *testing.T) {
	if err := seed.EnsureGeneralChannel(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil repo")
	}
}
