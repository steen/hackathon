-- 0005_dms_and_read_state: phase-9 schema for direct messages and
-- cross-surface read state, plus the two channel denorm columns the DM
-- and channel listings will read.
--
-- Decision-log (`lt show -p direct-messages 3`) anchors:
--   L1  conversation ids are ULID strings (TEXT primary keys, sortable).
--   L2  dm_conversations stores the participant pair canonically with
--       user_a_id < user_b_id; the UNIQUE(user_a_id, user_b_id) index
--       enforces "one conversation per pair" without needing OR-branches.
--   L3  read state lives in dedicated tables (channel_reads, dm_reads)
--       rather than columns on channels/dm_conversations, so the
--       (channel/conversation × user) cardinality is explicit.
--   L11 channels and dm_conversations both denormalize last_message_id
--       and last_message_at; listing endpoints read these directly to
--       avoid a sub-query per row.
--   L13 indexes target the listing access paths the L11 columns enable.
--   L23 single migration file for all phase-9 schema; do not split.
--
-- Asymmetric NOT NULL on read-state cursor columns is the load-bearing
-- detail (decision-log §11): channel_reads.last_read_message_id is
-- NOT NULL because GET /api/channels auto-materializes a row for every
-- channel on first listing, while dm_reads.last_read_dm_message_id is
-- NULLABLE because DM rows materialize lazily — NULL means "viewer has
-- never explicitly read this conversation" and the unread_count formula
-- COALESCEs over it.
--
-- The migration runner (apps/server/internal/db/migrate.go) wraps each
-- file in a single transaction and execs the body once, so this file
-- contains no BEGIN/COMMIT.

CREATE TABLE IF NOT EXISTS dm_conversations (
    id              TEXT PRIMARY KEY,
    user_a_id       TEXT NOT NULL REFERENCES users(id),
    user_b_id       TEXT NOT NULL REFERENCES users(id),
    last_message_id TEXT,
    last_message_at TIMESTAMP,
    created_at      TIMESTAMP NOT NULL,
    UNIQUE (user_a_id, user_b_id)
);

CREATE TABLE IF NOT EXISTS dm_messages (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES dm_conversations(id),
    sender_user_id  TEXT NOT NULL REFERENCES users(id),
    body            TEXT NOT NULL,
    created_at      TIMESTAMP NOT NULL
);

-- Hot path: paginated DM history for one conversation, newest-first.
-- Mirrors idx_messages_channel_created on the channel side.
CREATE INDEX IF NOT EXISTS idx_dm_messages_conv_created
    ON dm_messages(conversation_id, created_at);

CREATE TABLE IF NOT EXISTS channel_reads (
    channel_id           TEXT NOT NULL REFERENCES channels(id),
    user_id              TEXT NOT NULL REFERENCES users(id),
    last_read_message_id TEXT NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);

-- Listing-side join is `ON channel_id = c.id AND user_id = ?viewer`,
-- which the (channel_id, user_id) PK already covers. The complementary
-- index supports viewer-scoped scans (e.g. "all reads for one user")
-- that the channel-listing materialization does not need but that
-- future read-state queries may.
CREATE INDEX IF NOT EXISTS idx_channel_reads_user
    ON channel_reads(user_id);

CREATE TABLE IF NOT EXISTS dm_reads (
    conversation_id         TEXT NOT NULL REFERENCES dm_conversations(id),
    user_id                 TEXT NOT NULL REFERENCES users(id),
    last_read_dm_message_id TEXT,
    updated_at              TIMESTAMP NOT NULL,
    PRIMARY KEY (conversation_id, user_id)
);

ALTER TABLE channels ADD COLUMN last_message_id TEXT;
ALTER TABLE channels ADD COLUMN last_message_at TIMESTAMP;

-- Channel listing orders by last_message_at DESC; the index covers
-- both the ordering and the IS NOT NULL filter the listing query uses
-- to hide never-messaged channels (when applicable).
CREATE INDEX IF NOT EXISTS idx_channels_last_message_at
    ON channels(last_message_at);
