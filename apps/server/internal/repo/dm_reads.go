package repo

import (
	"context"
	"database/sql"
	"time"
)

// UpsertDMRead advances the (conversation_id, user_id) row in dm_reads
// to lastReadMessageID, but only when the supplied id is strictly
// greater than the persisted one. ULIDs sort lexicographically by
// time, so the string comparison in the WHERE clause is the
// chronological "newer than" test (decision log L5: advance-only — a
// POST /read with an older message_id is a silent no-op).
//
// The row may not yet exist (recipient who has never marked-read —
// dm_reads.last_read_dm_message_id is NULLABLE per migration 0005,
// decision-log §11), so the INSERT-then-conditional-UPDATE shape is
// used: the IGNORE handles a fresh (conversation_id, user_id) pair,
// landing it directly at lastReadMessageID; a pre-existing row only
// advances when strictly newer (the NULL → new path is also an
// advance because IS NULL is treated as "less than" by the predicate).
// Returns nil whether the row was inserted, advanced, or left
// unchanged — the caller cannot distinguish the no-op case from an
// advance, which matches the L5 idempotent client contract.
func (r *Repo) UpsertDMRead(ctx context.Context, conversationID, userID, lastReadMessageID string) error {
	now := time.Now().UTC()
	if _, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO dm_reads
		   (conversation_id, user_id, last_read_dm_message_id, updated_at)
		   VALUES (?, ?, ?, ?)`,
		conversationID, userID, lastReadMessageID, now,
	); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE dm_reads
		    SET last_read_dm_message_id = ?, updated_at = ?
		  WHERE conversation_id = ? AND user_id = ?
		    AND (last_read_dm_message_id IS NULL OR last_read_dm_message_id < ?)`,
		lastReadMessageID, now, conversationID, userID, lastReadMessageID,
	); err != nil {
		return err
	}
	return nil
}

// upsertDMReadAdvanceTx materializes (or advance-only updates) the
// sender's dm_reads row inside the InsertDMMessageTx transaction.
// Decision-log §11: the sender is implicitly caught up on their own
// send so unread_count for sender after a send equals 0. The advance-
// only UPDATE clause matches L5 / L21 — a row with a higher
// last_read_dm_message_id would be left alone (cannot happen for a
// fresh insert because messageID is a freshly-minted ULID, but the
// guard makes the predicate safe to reuse from the recipient
// `POST /api/dms/{id}/read` path which uses UpsertDMRead above).
//
// This function is package-internal so the transaction handle stays
// owned by InsertDMMessageTx; the recipient path uses UpsertDMRead.
func upsertDMReadAdvanceTx(ctx context.Context, tx *sql.Tx, conversationID, userID, messageID string, now time.Time) error {
	updated := now.UTC()

	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO dm_reads
		   (conversation_id, user_id, last_read_dm_message_id, updated_at)
		   VALUES (?, ?, ?, ?)`,
		conversationID, userID, messageID, updated,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE dm_reads
		    SET last_read_dm_message_id = ?, updated_at = ?
		  WHERE conversation_id = ? AND user_id = ?
		    AND (last_read_dm_message_id IS NULL OR last_read_dm_message_id < ?)`,
		messageID, updated, conversationID, userID, messageID,
	); err != nil {
		return err
	}

	return nil
}
