-- 0006_encryption: phase-10 schema for end-to-end encryption.
--
-- Decision-log (`lt -p e2e-encryption 3`) anchors:
--   §6 + §10  channel_members table — the explicit membership relation that
--             pins inviter signature + invitee pubkeys at invite time.
--   §7        per-recipient key wraps; channel_keys + dm_conversation_keys
--             store one wrapped root key per (channel_or_dm, member, gen).
--   L18       wipe-and-reset migration policy. Pre-encryption databases with
--             plaintext rows are rejected by the boot guard in apps/server/
--             boot.go before this file ever runs; the guard checks for the
--             cipher_suite column the migration adds below.
--   L23       envelope columns added to messages + dm_messages
--             (cipher_suite, key_generation_id, nonce, ciphertext,
--             sender_sign_pubkey, signature, client_created_at). Body is
--             retained NOT NULL for now; the cutover that drops body and
--             tightens these columns to NOT NULL ships in #983 (wave 6),
--             where every body consumer is rewritten in the same PR per L26.
--             Keeping body here lets the wave-2/3/4/5 PRs (members, keys,
--             ratelimit, identity) build and test against this schema
--             without a co-dependent rewrite of every messages/dm_messages
--             read path.
--   L24       channels.is_public flag ships with the repo.CreateChannel
--             signature change and the seed.go #general update in this same
--             PR — splitting them produces a window where #general is
--             is_public = FALSE and auto-join breaks.
--   L32       single migration file for the entire encryption epic; do not
--             split across the parallel sub-issues.
--   L33       partial index on channel_members(channel_id, user_id) WHERE
--             inviter_signature IS NULL. Forensic scan target only —
--             uniqueness is already covered by the table's primary key. The
--             NOT-NULL-for-private-channel rule lives in
--             apps/server/internal/repo/channel_members.go (SQLite CHECK
--             can't reference another table without a trigger).
--   L37       case-insensitive unique index on users.username so a pre-
--             encryption dev DB with mixed-case rows (e.g. Bob + BoB) is
--             refused at insert time. Changing the column collation in
--             place is not possible in SQLite without a table rebuild, and
--             rebuilding `users` requires foreign_keys=OFF which can't be
--             toggled inside a transaction; the unique index achieves the
--             same case-insensitive uniqueness without the rebuild dance.
--             The registration regex tightening in auth_handlers.go ships
--             with the user-identity sub-issue (#979).
--
-- The migration runner (apps/server/internal/db/migrate.go:applyOne) wraps
-- each file in one transaction and execs the body once, so this file
-- contains no BEGIN/COMMIT.

ALTER TABLE users ADD COLUMN box_pubkey  BLOB;
ALTER TABLE users ADD COLUMN sign_pubkey BLOB;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_nocase
    ON users (username COLLATE NOCASE);

ALTER TABLE channels ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE messages ADD COLUMN cipher_suite       INTEGER;
ALTER TABLE messages ADD COLUMN key_generation_id  INTEGER;
ALTER TABLE messages ADD COLUMN nonce              BLOB;
ALTER TABLE messages ADD COLUMN ciphertext         BLOB;
ALTER TABLE messages ADD COLUMN sender_sign_pubkey BLOB;
ALTER TABLE messages ADD COLUMN signature          BLOB;
ALTER TABLE messages ADD COLUMN client_created_at  TIMESTAMP;

ALTER TABLE dm_messages ADD COLUMN cipher_suite       INTEGER;
ALTER TABLE dm_messages ADD COLUMN key_generation_id  INTEGER;
ALTER TABLE dm_messages ADD COLUMN nonce              BLOB;
ALTER TABLE dm_messages ADD COLUMN ciphertext         BLOB;
ALTER TABLE dm_messages ADD COLUMN sender_sign_pubkey BLOB;
ALTER TABLE dm_messages ADD COLUMN signature          BLOB;
ALTER TABLE dm_messages ADD COLUMN client_created_at  TIMESTAMP;

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id          TEXT      NOT NULL REFERENCES channels(id),
    user_id             TEXT      NOT NULL REFERENCES users(id),
    inviter_user_id     TEXT      NOT NULL REFERENCES users(id),
    inviter_sign_pubkey BLOB      NOT NULL,
    inviter_signature   BLOB,
    invitee_box_pubkey  BLOB      NOT NULL,
    invitee_sign_pubkey BLOB      NOT NULL,
    added_at            TIMESTAMP NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_members_null_sig
    ON channel_members(channel_id, user_id)
    WHERE inviter_signature IS NULL;

CREATE TABLE IF NOT EXISTS channel_keys (
    channel_id        TEXT      NOT NULL REFERENCES channels(id),
    generation_id     INTEGER   NOT NULL,
    member_user_id    TEXT      NOT NULL REFERENCES users(id),
    wrapped_key       BLOB      NOT NULL,
    sender_box_pubkey BLOB      NOT NULL,
    nonce             BLOB      NOT NULL,
    created_at        TIMESTAMP NOT NULL,
    PRIMARY KEY (channel_id, generation_id, member_user_id)
);

CREATE TABLE IF NOT EXISTS dm_conversation_keys (
    conversation_id   TEXT      NOT NULL REFERENCES dm_conversations(id),
    member_user_id    TEXT      NOT NULL REFERENCES users(id),
    wrapped_key       BLOB      NOT NULL,
    sender_box_pubkey BLOB      NOT NULL,
    nonce             BLOB      NOT NULL,
    created_at        TIMESTAMP NOT NULL,
    PRIMARY KEY (conversation_id, member_user_id)
);
