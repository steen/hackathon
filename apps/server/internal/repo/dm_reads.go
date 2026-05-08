package repo

import (
	"context"
	"database/sql"
	"time"
)

// upsertDMReadAdvanceTx materializes (or advance-only updates) the
// sender's dm_reads row inside the InsertDMMessageTx transaction.
// Decision-log §11: the sender is implicitly caught up on their own
// send so unread_count for sender after a send equals 0. The advance-
// only UPDATE clause matches L5 / L21 — a row with a higher
// last_read_dm_message_id would be left alone (cannot happen for a
// fresh insert because messageID is a freshly-minted ULID, but the
// guard makes the predicate safe to reuse from the recipient
// `POST /api/dms/{id}/read` path landing in sub-issue #871).
//
// This function is package-internal so the transaction handle stays
// owned by InsertDMMessageTx; recipient-path callers in #871 will get
// their own non-tx variant alongside.
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
