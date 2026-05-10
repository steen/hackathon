// Tests for the channel_reads repo helpers added in issue #868. Lives
// in apps/server/internal/repo because the repo package is internal and
// external e2e packages cannot import it; the issue's footprint listed
// tests/e2e/phase-9/channel-reads-repo but `internal/` blocks that
// import. The PR body documents the deviation. G2 will add a black-box
// e2e once the HTTP handler exists.
package repo_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"hackathon/apps/server/internal/ids"
	"hackathon/apps/server/internal/repo"
)

// mustUserUnique seeds one user row with a username drawn from the
// random tail of a ULID — first 8 chars of a ULID are the millisecond
// timestamp, so two users created in the same ms collide on the
// shared `mustUser` helper. The last 16 chars are randomness; an 8-char
// slice from that tail is collision-resistant for tests creating a
// handful of users per process.
func mustUserUnique(t *testing.T, r *repo.Repo) string {
	t.Helper()
	id := ids.NewULID()
	if _, err := r.DB().ExecContext(context.Background(),
		`INSERT INTO users(id, username, password_hash, token_version, created_at)
		 VALUES (?, ?, '', 0, ?)`, id, "u-"+id[18:], time.Now()); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// AC: MaterializeChannelReadsTx inserts channel_reads rows for every
// channel where viewer lacks one, with last_read_message_id =
// channels.last_message_id. Channels with NULL last_message_id are
// skipped — the column is NOT NULL per migration 0005, and unread_count
// for a never-messaged channel is 0 either way.
func TestMaterializeChannelReadsTxPopulatesMissingRows(t *testing.T) {
	r, db := newRepo(t)
	viewer := mustUserUnique(t, r)
	author := mustUserUnique(t, r)

	chWithMsg := mustChannel(t, r, "alpha")
	chEmpty := mustChannel(t, r, "beta")
	joinAsMember(t, r, chWithMsg, viewer)
	joinAsMember(t, r, chEmpty, viewer)
	tipID := ids.NewULID()
	if _, err := r.InsertMessageTx(context.Background(), tipID, chWithMsg, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("InsertMessageTx: %v", err)
	}

	if err := r.MaterializeChannelReadsTx(context.Background(), viewer); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	var got string
	if err := db.QueryRow(
		`SELECT last_read_message_id FROM channel_reads WHERE channel_id = ? AND user_id = ?`,
		chWithMsg, viewer,
	).Scan(&got); err != nil {
		t.Fatalf("scan chWithMsg: %v", err)
	}
	if got != tipID {
		t.Fatalf("last_read_message_id: got %q want %q", got, tipID)
	}

	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM channel_reads WHERE channel_id = ? AND user_id = ?`,
		chEmpty, viewer,
	).Scan(&n); err != nil {
		t.Fatalf("count chEmpty: %v", err)
	}
	if n != 0 {
		t.Fatalf("never-messaged channel materialized %d rows; want 0", n)
	}
}

// AC: idempotent — re-running for the same viewer is a no-op (row count
// unchanged, baseline stays pinned to first-list tip even after the
// channel gets new messages — decision-log §11).
func TestMaterializeChannelReadsTxIsIdempotent(t *testing.T) {
	r, db := newRepo(t)
	viewer := mustUserUnique(t, r)
	author := mustUserUnique(t, r)
	channelID := mustChannel(t, r, "alpha")
	joinAsMember(t, r, channelID, viewer)
	firstTip := ids.NewULID()
	if _, err := r.InsertMessageTx(context.Background(), firstTip, channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("first message: %v", err)
	}

	if err := r.MaterializeChannelReadsTx(context.Background(), viewer); err != nil {
		t.Fatalf("first Materialize: %v", err)
	}

	// New message lands; channels.last_message_id advances. The
	// per-viewer baseline must NOT advance with the channel's tip, so
	// a second Materialize is a no-op.
	if _, err := r.InsertMessageTx(context.Background(), ids.NewULID(), channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("second message: %v", err)
	}

	if err := r.MaterializeChannelReadsTx(context.Background(), viewer); err != nil {
		t.Fatalf("second Materialize: %v", err)
	}

	var (
		rowCount int
		pinned   string
	)
	if err := db.QueryRow(
		`SELECT COUNT(*), MAX(last_read_message_id)
		   FROM channel_reads WHERE channel_id = ? AND user_id = ?`,
		channelID, viewer,
	).Scan(&rowCount, &pinned); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("row count: got %d want 1 after re-materialize", rowCount)
	}
	if pinned != firstTip {
		t.Fatalf("baseline drifted: got %q want %q (frozen at first-list)", pinned, firstTip)
	}
}

// AC: UpsertChannelRead with a newer message_id advances; with an older
// one is a silent no-op (decision-log L5 advance-only).
func TestUpsertChannelReadAdvanceOnly(t *testing.T) {
	r, db := newRepo(t)
	viewer := mustUserUnique(t, r)
	author := mustUserUnique(t, r)
	channelID := mustChannel(t, r, "alpha")
	older := ids.NewULID()
	if _, err := r.InsertMessageTx(context.Background(), older, channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("older: %v", err)
	}
	middle := ids.NewULID()
	if _, err := r.InsertMessageTx(context.Background(), middle, channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("middle: %v", err)
	}
	newer := ids.NewULID()
	if _, err := r.InsertMessageTx(context.Background(), newer, channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("newer: %v", err)
	}

	ctx := context.Background()

	// First call inserts at `middle`.
	if err := r.UpsertChannelRead(ctx, channelID, viewer, middle); err != nil {
		t.Fatalf("upsert middle: %v", err)
	}
	if got := readCursor(t, db, channelID, viewer); got != middle {
		t.Fatalf("after first upsert: got %q want %q", got, middle)
	}

	// Older id: silent no-op, cursor stays at middle.
	if err := r.UpsertChannelRead(ctx, channelID, viewer, older); err != nil {
		t.Fatalf("upsert older: %v", err)
	}
	if got := readCursor(t, db, channelID, viewer); got != middle {
		t.Fatalf("after older upsert: got %q want %q (must not regress)", got, middle)
	}

	// Newer id advances.
	if err := r.UpsertChannelRead(ctx, channelID, viewer, newer); err != nil {
		t.Fatalf("upsert newer: %v", err)
	}
	if got := readCursor(t, db, channelID, viewer); got != newer {
		t.Fatalf("after newer upsert: got %q want %q", got, newer)
	}

	// Same id (equal — not strictly greater) is a no-op.
	if err := r.UpsertChannelRead(ctx, channelID, viewer, newer); err != nil {
		t.Fatalf("upsert equal: %v", err)
	}
	if got := readCursor(t, db, channelID, viewer); got != newer {
		t.Fatalf("after equal upsert: got %q want %q", got, newer)
	}
}

// ListChannelsWithReadState reports the post-materialization unread
// count: zero for the viewer's pinned baseline, then matching the
// number of post-baseline messages.
func TestListChannelsWithReadStateReportsUnread(t *testing.T) {
	r, _ := newRepo(t)
	viewer := mustUserUnique(t, r)
	author := mustUserUnique(t, r)
	channelID := mustChannel(t, r, "alpha")
	joinAsMember(t, r, channelID, viewer)
	if _, err := r.InsertMessageTx(context.Background(), ids.NewULID(), channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("seed pre-baseline: %v", err)
	}

	if err := r.MaterializeChannelReadsTx(context.Background(), viewer); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	got, err := r.ListChannelsWithReadState(context.Background(), viewer)
	if err != nil {
		t.Fatalf("ListChannelsWithReadState: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len: got %d want 1", len(got))
	}
	if got[0].UnreadCount == nil || *got[0].UnreadCount != 0 {
		t.Fatalf("unread_count after materialize: got %v want 0", got[0].UnreadCount)
	}

	if _, err := r.InsertMessageTx(context.Background(), ids.NewULID(), channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("post-baseline 1: %v", err)
	}
	if _, err := r.InsertMessageTx(context.Background(), ids.NewULID(), channelID, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("post-baseline 2: %v", err)
	}

	got2, err := r.ListChannelsWithReadState(context.Background(), viewer)
	if err != nil {
		t.Fatalf("ListChannelsWithReadState 2: %v", err)
	}
	if got2[0].UnreadCount == nil || *got2[0].UnreadCount != 2 {
		t.Fatalf("unread_count after 2 new messages: got %v want 2", got2[0].UnreadCount)
	}
}

// AC for issue #938 — after the first ListChannelsWithReadState call
// (which runs MaterializeChannelReadsTx), every row representing a
// non-empty channel must carry a non-nil LastReadMessageID. This is
// the invariant that makes the unread_count subquery's COALESCE
// redundant: r.last_read_message_id is non-NULL on every row that
// could contribute a non-zero count, so `m.id > r.last_read_message_id`
// is well-defined without a fallback.
func TestListChannelsWithReadStateMaterializesNonEmptyChannels(t *testing.T) {
	r, _ := newRepo(t)
	viewer := mustUserUnique(t, r)
	author := mustUserUnique(t, r)

	withMsg := mustChannel(t, r, "alpha")
	empty := mustChannel(t, r, "beta")
	joinAsMember(t, r, withMsg, viewer)
	joinAsMember(t, r, empty, viewer)
	tip := ids.NewULID()
	if _, err := r.InsertMessageTx(context.Background(), tip, withMsg, author, fakeEnvelope(), time.Now()); err != nil {
		t.Fatalf("InsertMessageTx: %v", err)
	}

	got, err := r.ListChannelsWithReadState(context.Background(), viewer)
	if err != nil {
		t.Fatalf("ListChannelsWithReadState: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d want 2", len(got))
	}

	var sawWithMsg, sawEmpty bool
	for _, c := range got {
		switch c.ID {
		case withMsg:
			sawWithMsg = true
			if c.LastReadMessageID == nil {
				t.Fatalf("non-empty channel %q: LastReadMessageID nil — materialize-on-listing invariant broken", c.ID)
			}
			if *c.LastReadMessageID != tip {
				t.Fatalf("non-empty channel %q: LastReadMessageID = %q want %q", c.ID, *c.LastReadMessageID, tip)
			}
			if c.UnreadCount == nil || *c.UnreadCount != 0 {
				t.Fatalf("non-empty channel %q: UnreadCount = %v want 0 after first-list materialization", c.ID, c.UnreadCount)
			}
		case empty:
			sawEmpty = true
			if c.LastReadMessageID != nil {
				t.Fatalf("never-messaged channel %q: LastReadMessageID = %q want nil", c.ID, *c.LastReadMessageID)
			}
			if c.UnreadCount == nil || *c.UnreadCount != 0 {
				t.Fatalf("never-messaged channel %q: UnreadCount = %v want 0", c.ID, c.UnreadCount)
			}
		}
	}
	if !sawWithMsg || !sawEmpty {
		t.Fatalf("listing missing rows: sawWithMsg=%v sawEmpty=%v", sawWithMsg, sawEmpty)
	}
}

// Documents the MaterializeChannelReadsTx precondition called out in
// ListChannelsWithReadState's doc-comment: the listing SQL relies on
// `r.last_read_message_id` being non-NULL for every non-empty channel.
// Running the listing SQL directly without the materialize call (i.e.
// the LEFT JOIN misses) returns unread_count=0 for a channel that
// actually has messages — a silent undercount. This exercises the
// inner SELECT in isolation so the precondition stays load-bearing
// even if a future refactor moves the materialize call elsewhere.
func TestListChannelsWithReadStateRequiresMaterializeForNonEmpty(t *testing.T) {
	r, _ := newRepo(t)
	viewer := mustUserUnique(t, r)
	author := mustUserUnique(t, r)
	channelID := mustChannel(t, r, "alpha")
	for i := 0; i < 3; i++ {
		if _, err := r.InsertMessageTx(context.Background(), ids.NewULID(), channelID, author, fakeEnvelope(), time.Now()); err != nil {
			t.Fatalf("seed message %d: %v", i, err)
		}
	}

	// No MaterializeChannelReadsTx call — viewer has no channel_reads
	// row for this channel. Run the same SELECT body
	// ListChannelsWithReadState executes; the LEFT JOIN misses, so
	// `m.id > NULL` is NULL and COUNT(*) collapses to 0.
	row := r.DB().QueryRow(
		`SELECT (SELECT COUNT(*) FROM messages m
		          WHERE m.channel_id = c.id
		            AND m.id > r.last_read_message_id),
		        r.last_read_message_id IS NULL
		   FROM channels c
		   LEFT JOIN channel_reads r
		     ON r.channel_id = c.id AND r.user_id = ?
		  WHERE c.id = ?`,
		viewer, channelID,
	)
	var (
		unread     int
		joinMissed bool
	)
	if err := row.Scan(&unread, &joinMissed); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !joinMissed {
		t.Fatalf("test setup: channel_reads row already present; expected LEFT JOIN miss")
	}
	if unread != 0 {
		t.Fatalf("unread without materialize: got %d want 0 (silent undercount documents the precondition violation)", unread)
	}
}

func readCursor(t *testing.T, db *sql.DB, channelID, userID string) string {
	t.Helper()
	var got string
	if err := db.QueryRow(
		`SELECT last_read_message_id FROM channel_reads WHERE channel_id = ? AND user_id = ?`,
		channelID, userID,
	).Scan(&got); err != nil {
		t.Fatalf("scan cursor: %v", err)
	}
	return got
}
