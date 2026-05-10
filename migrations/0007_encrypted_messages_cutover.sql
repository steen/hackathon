-- 0007_encrypted_messages_cutover: phase-10 wave 6 — drop the plaintext
-- body column from messages + dm_messages and tighten the envelope
-- columns added by 0006 to NOT NULL. Per decision-log L21, L23, L26 +
-- specs/plans/phase-10/encryption.md, every channel message and DM is
-- now an Ed25519-signed naclbox-v1 envelope; there is no plaintext
-- fallback path after this migration.
--
-- 0006 left body NOT NULL alongside nullable envelope columns so that
-- the parallel wave-2/3/4/5 PRs (members, keys, ratelimit, identity)
-- could build and test against the schema without rewriting every
-- messages/dm_messages read path. This file completes the cutover.
--
-- SQLite cannot DROP COLUMN nor change a column's nullability in
-- place, so each table is rebuilt by the canonical four-step dance:
--   1. CREATE TABLE <new> with the post-cutover shape (envelope
--      columns NOT NULL, body absent).
--   2. INSERT INTO <new> SELECT ... FROM <old> — preserves every row
--      already written through the wave-2..5 envelope path.
--   3. DROP TABLE <old>.
--   4. ALTER TABLE <new> RENAME TO <old>.
--
-- The L18 boot guard in apps/server/boot.go runs BEFORE this file ever
-- executes against any pre-encryption DB; that guard already aborts
-- startup when messages or dm_messages have rows but cipher_suite is
-- absent. By the time 0007 runs, every row in messages / dm_messages
-- carries a populated envelope (post-0006 + the same-PR repo + handler
-- rewrite — the cutover is atomic at the application layer).
--
-- The supporting indexes are recreated against the rebuilt tables —
-- SQLite does not migrate indexes through the rename. Only the indexes
-- that 0001/0002/0005/0006 created on these tables are reproduced;
-- foreign-key references from other tables (channel_reads, dm_reads,
-- channels.last_message_id, dm_conversations.last_message_id) are by
-- TEXT id and do not need recreation.
--
-- The migration runner (apps/server/internal/db/migrate.go:applyOne)
-- wraps each file in one transaction, so this whole rebuild lands
-- atomically.

CREATE TABLE messages_new (
    id                 TEXT      PRIMARY KEY,
    channel_id         TEXT      NOT NULL REFERENCES channels(id),
    user_id            TEXT      NOT NULL REFERENCES users(id),
    cipher_suite       INTEGER   NOT NULL,
    key_generation_id  INTEGER   NOT NULL,
    nonce              BLOB      NOT NULL,
    ciphertext         BLOB      NOT NULL,
    sender_sign_pubkey BLOB      NOT NULL,
    signature          BLOB      NOT NULL,
    client_created_at  TIMESTAMP NOT NULL,
    created_at         TIMESTAMP NOT NULL
);

INSERT INTO messages_new(
    id, channel_id, user_id,
    cipher_suite, key_generation_id, nonce, ciphertext,
    sender_sign_pubkey, signature, client_created_at, created_at
)
SELECT
    id, channel_id, user_id,
    cipher_suite, key_generation_id, nonce, ciphertext,
    sender_sign_pubkey, signature, client_created_at, created_at
  FROM messages;

DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;

CREATE INDEX IF NOT EXISTS idx_messages_channel_created
    ON messages(channel_id, created_at);

CREATE INDEX IF NOT EXISTS idx_messages_channel_id
    ON messages(channel_id, id);

CREATE TABLE dm_messages_new (
    id                 TEXT      PRIMARY KEY,
    conversation_id    TEXT      NOT NULL REFERENCES dm_conversations(id),
    sender_user_id     TEXT      NOT NULL REFERENCES users(id),
    cipher_suite       INTEGER   NOT NULL,
    key_generation_id  INTEGER   NOT NULL,
    nonce              BLOB      NOT NULL,
    ciphertext         BLOB      NOT NULL,
    sender_sign_pubkey BLOB      NOT NULL,
    signature          BLOB      NOT NULL,
    client_created_at  TIMESTAMP NOT NULL,
    created_at         TIMESTAMP NOT NULL
);

INSERT INTO dm_messages_new(
    id, conversation_id, sender_user_id,
    cipher_suite, key_generation_id, nonce, ciphertext,
    sender_sign_pubkey, signature, client_created_at, created_at
)
SELECT
    id, conversation_id, sender_user_id,
    cipher_suite, key_generation_id, nonce, ciphertext,
    sender_sign_pubkey, signature, client_created_at, created_at
  FROM dm_messages;

DROP TABLE dm_messages;
ALTER TABLE dm_messages_new RENAME TO dm_messages;

CREATE INDEX IF NOT EXISTS idx_dm_messages_conv_created
    ON dm_messages(conversation_id, created_at);
