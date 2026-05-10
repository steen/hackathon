package repo_test

import (
	"context"
	"testing"
	"time"

	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

func mustChannel(t *testing.T, r *repo.Repo, name string) string {
	t.Helper()
	ch, err := r.CreateChannel(context.Background(), ids.NewULID(), name, false, time.Now())
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	return ch.ID
}

func mustUser(t *testing.T, r *repo.Repo) string {
	t.Helper()
	id := ids.NewULID()
	_, err := r.DB().ExecContext(context.Background(),
		`INSERT INTO users(id, username, password_hash, token_version, created_at)
		 VALUES (?, ?, '', 0, ?)`, id, "u-"+id[:8], time.Now())
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// US-5 — InsertMessageTx persists and returns the row.
func TestInsertMessageTxPersistsRow(t *testing.T) {
	r, db := newRepo(t)
	chID := mustChannel(t, r, "general")
	uid := mustUser(t, r)

	id := ids.NewULID()
	env := fakeEnvelope()
	m, err := r.InsertMessageTx(context.Background(), id, chID, uid, env, time.Now())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if m.ID != id || m.ChannelID != chID || m.Envelope.CipherSuite != 0x01 {
		t.Fatalf("returned: %+v", m)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rows: got %d want 1", n)
	}
}

// US-6 — ListMessages returns newest-first and supports the `before`
// cursor for pagination.
func TestListMessagesReturnsNewestFirstAndPaginates(t *testing.T) {
	r, _ := newRepo(t)
	chID := mustChannel(t, r, "general")
	uid := mustUser(t, r)

	var ids26 [5]string
	for i := range ids26 {
		id := ids.NewULID()
		if _, err := r.InsertMessageTx(context.Background(), id, chID, uid, fakeEnvelope(), time.Now()); err != nil {
			t.Fatalf("Insert[%d]: %v", i, err)
		}
		ids26[i] = id
	}

	got, err := r.ListMessages(context.Background(), chID, "", 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len: got %d want 3", len(got))
	}
	if got[0].ID != ids26[4] || got[1].ID != ids26[3] || got[2].ID != ids26[2] {
		t.Fatalf("order: got %v want newest first", []string{got[0].ID, got[1].ID, got[2].ID})
	}

	page2, err := r.ListMessages(context.Background(), chID, got[2].ID, 3)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len: got %d want 2", len(page2))
	}
	if page2[0].ID != ids26[1] || page2[1].ID != ids26[0] {
		t.Fatalf("page2 order: got %v want %v,%v",
			[]string{page2[0].ID, page2[1].ID}, ids26[1], ids26[0])
	}
}

// ListMessages caps an absurdly high limit at MaxMessagesLimit.
func TestListMessagesCapsLimit(t *testing.T) {
	r, _ := newRepo(t)
	chID := mustChannel(t, r, "general")
	uid := mustUser(t, r)
	for i := 0; i < 5; i++ {
		_, _ = r.InsertMessageTx(context.Background(), ids.NewULID(), chID, uid, fakeEnvelope(), time.Now())
	}
	got, err := r.ListMessages(context.Background(), chID, "", 9999)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("len: got %d want 5", len(got))
	}
}
