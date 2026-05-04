---
feature: sqlite-schema-and-ulid
phase: phase-1
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: SQLite schema and ULID generation

**Spec:** `specs/plans/phase-1/feature-sqlite-schema-and-ulid.md`
**Implementation status:** implemented — `migrations/0001_init.sql` defines the schema; `apps/server/internal/db/migrate.go` runs migrations; `apps/server/internal/ids/ulid.go` exposes `NewULID()`. Migrations apply on startup at `apps/server/main.go:85-90` (`appdb.Apply(migCtx, sqlDB)`).
**E2E test directory:** `tests/e2e/phase-1/sqlite-schema-and-ulid/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `migrations/0001_init.sql` defines tables for users, channels, messages, and `auth_events`. | missing | — |
| AC-2 | The `users` table includes a `token_version` column (used by US-12 logout-invalidation). | missing | — |
| AC-3 | A migration runner applies pending migrations on server startup. | missing | — |
| AC-4 | ULIDs are used as primary keys for users, channels, and messages. | missing | — |
| AC-5 | A package exposes a `NewULID()` helper (monotonic where possible). | missing | — |

## Findings

### Missing E2E tests

**AC-1 — schema defines users, channels, messages, auth_events**
- **What to assert:** Boot the binary against a fresh `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Wait for port. Open the SQLite file read-only via `database/sql` driver `modernc.org/sqlite`. `SELECT name FROM sqlite_master WHERE type='table' ORDER BY name` → assert the result set includes `users`, `channels`, `messages`, `auth_events` (and `schema_migrations`, see AC-3). For each of the four data tables, `PRAGMA table_info(<name>)` and assert at minimum the columns the spec calls out:
  - `users(id, username, password_hash, token_version, created_at)`
  - `channels(id, name, created_at)`
  - `messages(id, channel_id, user_id, body, created_at)`
  - `auth_events(id, user_id, kind, ip, ua, at)`
  Also `SELECT sql FROM sqlite_master WHERE type='index'` and assert at least one index whose `tbl_name='messages'` covers `(channel_id, created_at)` and one for `auth_events(user_id, at)`.
- **Layer:** Go (boot binary, sqlite read).
- **File path:** `tests/e2e/phase-1/sqlite-schema-and-ulid/schema_tables_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `openDBReadOnly(t, srv) *sql.DB`.

**AC-2 — `users.token_version` exists and increments on logout**
- **What to assert:** From AC-1's schema dump, assert the `users` table has a `token_version` column with type `INTEGER` and a `NOT NULL` constraint (verify default value `0` from the migration). Behavioural half: register `alice`; open SQLite read-only and `SELECT token_version FROM users WHERE username='alice'` → assert 0. Login → logout (via the auth handlers); re-query → assert 1. Logout again from a fresh login → assert 2.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/sqlite-schema-and-ulid/token_version_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `register`, `login`, `logout`, `openDBReadOnly`.

**AC-3 — migration runner applies pending migrations on startup**
- **What to assert:** Boot against a fresh `CHAT_DB_PATH`; open the file read-only; assert `schema_migrations` table exists; `SELECT version FROM schema_migrations ORDER BY version` → assert it contains at least `1` (the `0001_init` migration; verify the exact version key format by reading `db/migrate.go` once during test authoring — could be int 1, string "0001", or filename). Stop the server. Boot again against the *same* file → no error, port listens, `SELECT count(*) FROM schema_migrations` returns the same count (idempotent). Apply a hand-authored second migration row to force a re-run scenario? Not needed for E2E; the idempotency check above is sufficient end-to-end signal.
- **Layer:** Go (boot binary twice, sqlite read).
- **File path:** `tests/e2e/phase-1/sqlite-schema-and-ulid/migrations_applied_test.go`.
- **Setup it needs:** same as AC-1; harness must allow stopping and restarting against the same `CHAT_DB_PATH`.
- **Helpers it can reuse:** harness; `openDBReadOnly`; introduce `restartServer(t, srv)` that cancels and re-execs against the existing DB file.

**AC-4 — ULIDs as PKs for users, channels, messages**
- **What to assert:** Register a user → query `SELECT id FROM users WHERE username='alice'` → assert the id matches `^[0-9A-HJKMNP-TV-Z]{26}$` (Crockford base32, 26 chars). Create a channel `c1` → `SELECT id FROM channels WHERE name='c1'` → same regex. Send a message → `SELECT id FROM messages ORDER BY id DESC LIMIT 1` → same regex. Send 5 messages in rapid succession → assert their ids are strictly lexicographically increasing (which is the ULID sort property, also covered by AC-5 but cheap to add here). Confirm none of the three tables ever yields an INTEGER autoincrement id.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/sqlite-schema-and-ulid/ulid_pks_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `register`, `login`, `createChannel`, `sendMessage`, `openDBReadOnly`.

**AC-5 — `NewULID()` helper, monotonic across rapid calls**
- **What to assert:** This is a unit-level invariant on `apps/server/internal/ids/ulid.go`. From an E2E vantage the only proxy is "messages POST'd in rapid succession have strictly increasing ids," already covered by AC-4. To pin AC-5 explicitly: send 100 messages back-to-back in a tight loop via `POST /api/channels/{id}/messages`; collect each response's `data.message.id`; assert all 100 are unique AND the slice is strictly lexicographically increasing (`sort.StringsAreSorted(ids)` AND no duplicate). The monotonic-ULID property under concurrent calls is a stronger claim — fan out 10 goroutines each sending 50 messages; assert all 500 ids are unique and decode to ULIDs whose timestamp components are within 5 seconds of `time.Now()`. (Decoding the ULID timestamp is straightforward — the first 10 chars are the milliseconds-since-epoch in Crockford base32.)
- **Layer:** Go (boot binary, HTTP, concurrent).
- **File path:** `tests/e2e/phase-1/sqlite-schema-and-ulid/ulid_monotonic_test.go`.
- **Setup it needs:** same as AC-1; this is the slowest test — gate with `if testing.Short() { t.Skip() }` if 500 messages is too much.
- **Helpers it can reuse:** harness; `sendMessage`; introduce `decodeULIDTimestamp(s string) time.Time` if not already in harness.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. Add a `restartServer(t, srv) *runningServer` helper for AC-3 — the stop/start dance against the same `CHAT_DB_PATH` is reusable.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/sqlite-schema-and-ulid/harness_test.go` with copied helpers + `register`, `login`, `logout`, `createChannel`, `sendMessage`, `openDBReadOnly`, `restartServer`, `decodeULIDTimestamp`.
- Add `schema_tables_test.go` (AC-1), `token_version_test.go` (AC-2), `migrations_applied_test.go` (AC-3), `ulid_pks_test.go` (AC-4), `ulid_monotonic_test.go` (AC-5, gated on `!testing.Short()` if needed).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
