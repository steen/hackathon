package repo

import (
	"context"
	"time"
)

// UpsertChannelRead advances the (channel_id, user_id) row in
// channel_reads to lastReadMessageID, but only when the supplied id is
// strictly greater than the persisted one. ULIDs sort lexicographically
// by time, so the string comparison in the WHERE clause is the
// chronological "newer than" test (decision log `lt -p direct-messages
// 3` L5: advance-only — a POST /read with an older message_id is a
// silent no-op).
//
// The INSERT-then-conditional-UPDATE shape uses ON CONFLICT so a fresh
// (channel_id, user_id) pair lands at lastReadMessageID, and a
// pre-existing row only advances. Returns nil whether the row was
// inserted, advanced, or left unchanged — the caller cannot distinguish
// the no-op case from an advance, which matches the L5 idempotent
// client contract.
func (r *Repo) UpsertChannelRead(ctx context.Context, channelID, userID, lastReadMessageID string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO channel_reads (channel_id, user_id, last_read_message_id, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(channel_id, user_id) DO UPDATE SET
		     last_read_message_id = excluded.last_read_message_id,
		     updated_at           = excluded.updated_at
		   WHERE excluded.last_read_message_id > channel_reads.last_read_message_id`,
		channelID, userID, lastReadMessageID, now,
	)
	return err
}

// MaterializeChannelReadsTx inserts a channel_reads row for every
// channel where viewerUserID lacks one, freezing the baseline at each
// channel's current last_message_id (decision log §11: "auto-materialize
// on listing"). Channels with NULL last_message_id are skipped — their
// unread_count is zero regardless of materialization, and the
// channel_reads.last_read_message_id column is NOT NULL per migration
// 0005, so writing NULL would violate the schema.
//
// Hot-path pre-check: every GET /api/channels lands here, but after
// the first listing the viewer is fully materialized and the INSERT
// would write zero rows. Compare two cheap counts (viewer's
// channel_reads rows vs. channels with a tip). When they match, skip
// the BEGIN/COMMIT entirely — issue #937. The race window where a new
// channel's first message lands between the two SELECTs is harmless:
// the next GET resolves the gap, and unread_count for an
// un-materialized channel still computes correctly via COALESCE in
// the listing SELECT.
//
// The sweep runs inside one BeginTx → ExecContext → Commit
// transaction, mirroring auth_store.go:81 (decision log L21). The
// transaction makes the materialization atomic with a future listing
// SELECT that joins channel_reads in the same tx (G2), but it is also
// correct in isolation: re-running for the same viewer is a no-op
// because the WHERE NOT EXISTS clause filters out already-materialized
// rows.
func (r *Repo) MaterializeChannelReadsTx(ctx context.Context, viewerUserID string) error {
	// Phase-10 L25: both the pre-check COUNT and the sweep INSERT join
	// channel_members so the materialization only touches channels the
	// viewer is a member of. Without the filter, the sweep keeps creating
	// channel_reads rows for channels the viewer cannot see (and the
	// pre-check's tip-count diverges from the reads-count, forcing a
	// pointless transaction every listing). adversarial-review-2 RACE-3.
	var readsCount, memberChannelsWithTipCount int
	if err := r.db.QueryRowContext(ctx,
		`SELECT
		     (SELECT COUNT(*) FROM channel_reads WHERE user_id = ?),
		     (SELECT COUNT(*)
		        FROM channels c
		        JOIN channel_members cm
		          ON cm.channel_id = c.id AND cm.user_id = ?
		       WHERE c.last_message_id IS NOT NULL)`,
		viewerUserID, viewerUserID,
	).Scan(&readsCount, &memberChannelsWithTipCount); err != nil {
		return err
	}
	if readsCount >= memberChannelsWithTipCount {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO channel_reads (channel_id, user_id, last_read_message_id, updated_at)
		 SELECT c.id, ?, c.last_message_id, ?
		   FROM channels c
		   JOIN channel_members cm
		     ON cm.channel_id = c.id AND cm.user_id = ?
		  WHERE c.last_message_id IS NOT NULL
		    AND NOT EXISTS (
		        SELECT 1 FROM channel_reads
		         WHERE channel_id = c.id AND user_id = ?
		    )`,
		viewerUserID, now, viewerUserID, viewerUserID,
	); err != nil {
		return err
	}
	return tx.Commit()
}
