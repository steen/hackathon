// Tests for the dm_reads repo helpers added in issue #871. Lives in
// apps/server/internal/repo because the repo package is internal and
// external e2e packages cannot import it; the recipient-path
// black-box e2e lives under tests/e2e/phase-9/dms-read-state.
package repo_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// seedConversation registers two users and a conversation row, returning
// the conversation id and the (userA, userB) ids in canonical order.
func seedConversation(t *testing.T, r *repo.Repo) (convID, userA, userB string) {
	t.Helper()
	userA = mustUserUnique(t, r)
	userB = mustUserUnique(t, r)
	if userA > userB {
		userA, userB = userB, userA
	}
	convID = ids.NewULID()
	if _, _, err := r.FindOrCreateDMConversation(context.Background(), convID, userA, userB, time.Now()); err != nil {
		t.Fatalf("FindOrCreateDMConversation: %v", err)
	}
	return convID, userA, userB
}

func dmReadCursor(t *testing.T, db *sql.DB, conversationID, userID string) (string, bool) {
	t.Helper()
	var got sql.NullString
	if err := db.QueryRow(
		`SELECT last_read_dm_message_id FROM dm_reads
		   WHERE conversation_id = ? AND user_id = ?`,
		conversationID, userID,
	).Scan(&got); err != nil {
		if err == sql.ErrNoRows {
			return "", false
		}
		t.Fatalf("scan cursor: %v", err)
	}
	return got.String, got.Valid
}

// AC: first call materializes the recipient's row at the supplied
// message id (decision-log §11: recipient's row is created only by an
// explicit POST /read).
func TestUpsertDMReadMaterializesRecipientRow(t *testing.T) {
	r, db := newRepo(t)
	conv, _, recipient := seedConversation(t, r)

	mid := ids.NewULID()
	if err := r.UpsertDMRead(context.Background(), conv, recipient, mid); err != nil {
		t.Fatalf("UpsertDMRead: %v", err)
	}

	got, ok := dmReadCursor(t, db, conv, recipient)
	if !ok {
		t.Fatalf("dm_reads row missing after first UpsertDMRead")
	}
	if got != mid {
		t.Errorf("first upsert: got %q want %q", got, mid)
	}
}

// AC: advance-only — older message_id is a silent no-op (L5).
func TestUpsertDMReadAdvanceOnly(t *testing.T) {
	r, db := newRepo(t)
	conv, _, recipient := seedConversation(t, r)

	older := ids.NewULID()
	time.Sleep(2 * time.Millisecond)
	middle := ids.NewULID()
	time.Sleep(2 * time.Millisecond)
	newer := ids.NewULID()

	ctx := context.Background()
	if err := r.UpsertDMRead(ctx, conv, recipient, middle); err != nil {
		t.Fatalf("upsert middle: %v", err)
	}
	if got, _ := dmReadCursor(t, db, conv, recipient); got != middle {
		t.Fatalf("after middle: got %q want %q", got, middle)
	}

	if err := r.UpsertDMRead(ctx, conv, recipient, older); err != nil {
		t.Fatalf("upsert older: %v", err)
	}
	if got, _ := dmReadCursor(t, db, conv, recipient); got != middle {
		t.Errorf("after older: got %q want %q (must not regress)", got, middle)
	}

	if err := r.UpsertDMRead(ctx, conv, recipient, newer); err != nil {
		t.Fatalf("upsert newer: %v", err)
	}
	if got, _ := dmReadCursor(t, db, conv, recipient); got != newer {
		t.Errorf("after newer: got %q want %q", got, newer)
	}

	if err := r.UpsertDMRead(ctx, conv, recipient, newer); err != nil {
		t.Fatalf("upsert equal: %v", err)
	}
	if got, _ := dmReadCursor(t, db, conv, recipient); got != newer {
		t.Errorf("after equal: got %q want %q (no-op)", got, newer)
	}
}

// AC: existing NULL last_read_dm_message_id is treated as "less than"
// any ULID and is advanced by the first UpsertDMRead call. The NULL
// row can pre-exist legitimately if a prior code path inserted it
// without a pointer (defensive: there is no such path today, but the
// schema allows NULL per migration 0005, so the helper must handle it).
func TestUpsertDMReadAdvancesFromNULL(t *testing.T) {
	r, db := newRepo(t)
	conv, _, recipient := seedConversation(t, r)

	// Pre-seed a NULL row directly to model the "row exists but pointer
	// is NULL" edge case the schema permits.
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO dm_reads(conversation_id, user_id, last_read_dm_message_id, updated_at)
		 VALUES (?, ?, NULL, ?)`,
		conv, recipient, time.Now().UTC()); err != nil {
		t.Fatalf("seed NULL row: %v", err)
	}

	mid := ids.NewULID()
	if err := r.UpsertDMRead(context.Background(), conv, recipient, mid); err != nil {
		t.Fatalf("UpsertDMRead: %v", err)
	}
	got, ok := dmReadCursor(t, db, conv, recipient)
	if !ok || got != mid {
		t.Errorf("after NULL→mid: got=%q ok=%v want %q", got, ok, mid)
	}
}
