-- 0002_messages_cursor_index: explicit covering index for the message
-- pagination cursor (`WHERE channel_id = ? AND id < ? ORDER BY id DESC`).
--
-- The query plan is already fine on the current schema because SQLite uses
-- the implicit index behind the TEXT PRIMARY KEY on `messages.id`. This index
-- is added so that the plan no longer depends on that incidental coverage:
-- a future schema change (e.g. dropping the PK or switching to AUTOINCREMENT)
-- would otherwise silently regress paginated history to a table scan.
-- Filed against the security-audit info finding in #78.

CREATE INDEX IF NOT EXISTS idx_messages_channel_id
    ON messages(channel_id, id);
