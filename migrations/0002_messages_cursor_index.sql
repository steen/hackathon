-- 0002_messages_cursor_index: explicit composite index for the message
-- pagination cursor (`WHERE channel_id = ? AND id < ? ORDER BY id DESC`).
--
-- The pre-existing `idx_messages_channel_created (channel_id, created_at)`
-- only covers the `channel_id =` equality, not the `id <` range or the
-- `ORDER BY id DESC`; the implicit PK index on `messages.id` doesn't
-- co-locate by `channel_id`. This composite gives the planner a strict
-- optimum without relying on incidental PK coverage, and stays correct if
-- the PK definition is ever revised.
-- Filed against the security-audit info finding in #78.

CREATE INDEX IF NOT EXISTS idx_messages_channel_id
    ON messages(channel_id, id);
