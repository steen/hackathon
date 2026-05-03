-- 0001_init: baseline schema for users, channels, messages, and the
-- auth_events audit log. ULID strings are stored as TEXT primary keys so the
-- natural lexicographic order matches creation order; this is the property
-- that lets paginated history use the message id directly as a cursor.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    -- token_version is bumped on logout (and any future password change) to
    -- invalidate every previously-issued JWT for this user (see PRD §9, US-12).
    token_version INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS channels (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES channels(id),
    user_id    TEXT NOT NULL REFERENCES users(id),
    body       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);

-- Hot path: paginated history for one channel ordered by time / ULID.
CREATE INDEX IF NOT EXISTS idx_messages_channel_created
    ON messages(channel_id, created_at);

-- user_id is nullable: failed logins for an unknown username have no user row
-- yet still need to be recorded for the audit log (PRD §9).
CREATE TABLE IF NOT EXISTS auth_events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT,
    kind    TEXT NOT NULL,
    ip      TEXT,
    ua      TEXT,
    at      TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_auth_events_user_at
    ON auth_events(user_id, at);
