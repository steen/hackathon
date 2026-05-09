package repo_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	appdb "hackathon/apps/server/internal/db"
	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
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

// US-4 — CreateChannel persists and returns the row.
func TestCreateChannelPersistsAndReturnsIt(t *testing.T) {
	r, db := newRepo(t)

	id := ids.NewULID()
	ch, err := r.CreateChannel(context.Background(), id, "general", false, time.Now())
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.ID != id || ch.Name != "general" {
		t.Fatalf("returned channel: %+v", ch)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM channels WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows: got %d want 1", n)
	}
}

// US-4 — duplicate name is rejected with the typed sentinel.
func TestCreateChannelRejectsDuplicateName(t *testing.T) {
	r, _ := newRepo(t)
	if _, err := r.CreateChannel(context.Background(), ids.NewULID(), "dup", false, time.Now()); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := r.CreateChannel(context.Background(), ids.NewULID(), "dup", false, time.Now())
	if !errors.Is(err, repo.ErrChannelNameTaken) {
		t.Fatalf("second err: got %v want ErrChannelNameTaken", err)
	}
}

// US-3 — ListChannels returns rows ordered chronologically by id.
func TestListChannelsReturnsSeededChannels(t *testing.T) {
	r, _ := newRepo(t)
	a, _ := r.CreateChannel(context.Background(), ids.NewULID(), "alpha", false, time.Now())
	b, _ := r.CreateChannel(context.Background(), ids.NewULID(), "beta", false, time.Now())
	got, err := r.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2", len(got))
	}
	if got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("order: got %v,%v want %v,%v", got[0].ID, got[1].ID, a.ID, b.ID)
	}
}

func TestGetChannelReturnsNilForMissing(t *testing.T) {
	r, _ := newRepo(t)
	got, err := r.GetChannel(context.Background(), ids.NewULID())
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v want nil", got)
	}
}

func TestRenameChannelPersistsNewName(t *testing.T) {
	r, db := newRepo(t)
	created, err := r.CreateChannel(context.Background(), ids.NewULID(), "old", false, time.Now())
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	got, err := r.RenameChannel(context.Background(), created.ID, "new", time.Now())
	if err != nil {
		t.Fatalf("RenameChannel: %v", err)
	}
	if got.ID != created.ID || got.Name != "new" {
		t.Fatalf("returned: %+v want id=%s name=new", got, created.ID)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM channels WHERE id = ?`, created.ID).Scan(&name); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if name != "new" {
		t.Fatalf("persisted name: got %q want new", name)
	}
}

func TestRenameChannelReturnsNotFound(t *testing.T) {
	r, _ := newRepo(t)
	_, err := r.RenameChannel(context.Background(), ids.NewULID(), "anything", time.Now())
	if !errors.Is(err, repo.ErrChannelNotFound) {
		t.Fatalf("err: got %v want ErrChannelNotFound", err)
	}
}

func TestRenameChannelReturnsNameTakenOnCollision(t *testing.T) {
	r, _ := newRepo(t)
	a, err := r.CreateChannel(context.Background(), ids.NewULID(), "alpha", false, time.Now())
	if err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if _, err := r.CreateChannel(context.Background(), ids.NewULID(), "beta", false, time.Now()); err != nil {
		t.Fatalf("create beta: %v", err)
	}
	_, err = r.RenameChannel(context.Background(), a.ID, "beta", time.Now())
	if !errors.Is(err, repo.ErrChannelNameTaken) {
		t.Fatalf("err: got %v want ErrChannelNameTaken", err)
	}
}
