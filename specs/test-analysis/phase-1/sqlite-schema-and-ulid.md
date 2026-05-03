---
feature: sqlite-schema-and-ulid
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 5
covered: 5
partial: 0
missing: 0
deferred: 0
---

# Test analysis: SQLite schema and ULID generation

**Spec:** `specs/plans/phase-1/feature-sqlite-schema-and-ulid.md`
**Implementation status:** implemented — `migrations/0001_init.sql` (embedded via `migrations/embed.go`) defines users/channels/messages/auth_events with the required columns and indexes. `apps/server/internal/db/{open,migrate}.go` open the DB (calling `EnsureFile` for SEC-14 0600 perms), apply migrations idempotently, and `apps/server/main.go` wires both into startup behind `CHAT_DB_PATH`. `apps/server/internal/ids/ulid.go` exposes `NewULID()` with a thread-safe monotonic source. AC-4's previously-partial flag is now closed: `feature-channels-and-messages` (PR #42, wiring closed by gap-C/D batch in `fa60bfd`) ships INSERT call sites that mint via `ids.NewULID()` and routes are live on the mux.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `migrations/0001_init.sql` defines tables for users, channels, messages, and `auth_events`. | covered | `apps/server/internal/db/migrate_test.go::TestApplyAppliesEmbeddedMigrations` (queries `sqlite_master` for all four tables + `schema_migrations`). |
| AC-2 | The `users` table includes a `token_version` column (used by US-12 logout-invalidation). | covered | `apps/server/internal/db/migrate_test.go::TestApplyAppliesEmbeddedMigrations` (explicit `PRAGMA table_info(users)` walk asserts `token_version` is present). |
| AC-3 | A migration runner applies pending migrations on server startup. | covered | `apps/server/internal/db/migrate_test.go::TestApplyIsIdempotent` + `TestApplyFSAppliesPendingOnly` + `TestApplyRejectsNilDB` (runner is idempotent, applies pending-only, rejects nil DB). Wired into startup at `apps/server/main.go:48-53` behind `CHAT_DB_PATH`. |
| AC-4 | ULIDs are used as primary keys for users, channels, and messages. | covered | `apps/server/internal/http/channels_handlers.go:77` and `messages_handlers.go:138` are the load-bearing INSERT call sites — both `id := ids.NewULID()`. With `apps/server/main.go:133` wiring `ch.Routes(mux, require, msg)`, those handlers are reachable on the live mux. Behavior verified end-to-end by `messages_handlers_test.go::TestPostMessagePersistsAndBroadcasts` and `channels_handlers_test.go::TestCreateChannelPersistsAndReturnsIt` (both assert the persisted row's `id` is the canonical 26-char Crockford-base32 ULID shape `NewULID()` emits). The users-table INSERT path (auth registration) is covered separately by `auth_handlers_test.go::TestRegisterCreatesUserWithInviteCode`. |
| AC-5 | A package exposes a `NewULID()` helper (monotonic where possible). | covered | `apps/server/internal/ids/ulid_test.go::TestNewULIDLength` (26 chars, Crockford-base32) + `TestNewULIDIsLexicographicallyIncreasing` (10k strict-greater chain) + `TestNewULIDIsUnique` (10k no-collision) + `TestNewULIDConcurrentSafe` (32 goroutines × 1k under -race). |

## Findings

### Covered

- **AC-1, AC-2** — `TestApplyAppliesEmbeddedMigrations` checks both the table set and the specific column the spec calls out for US-12. The `IF NOT EXISTS` clauses in the SQL plus the `schema_migrations` ledger together make the runner idempotent at two layers (SQL-level + ledger-level), which the idempotency test verifies.
- **AC-3** — Three tests cover the runner: applies-once, applies-pending-only, rejects-nil. The startup wiring (`appdb.Apply(migCtx, sqlDB)` with a 30s timeout) is reachable when `CHAT_DB_PATH` is set; phase-0 boot path (`scripts/smoke.sh`) intentionally leaves the env var unset and gets the no-DB code path.
- **AC-4 (re-promoted from partial)** — at the previous SHA the schema permitted ULIDs but no INSERT site used `NewULID()`. The `feature-channels-and-messages` PR introduced the INSERT call sites (channels_handlers.go:77, messages_handlers.go:138) and the gap-C/D batch wired the handlers to the live mux at main.go:133. The data-layer contract "all PK inserts use NewULID" is now real and reachable from a live request. A static grep test enumerating every INSERT site is still the right defensive next move (see Recommendations) but is out of scope for this run.
- **AC-5** — Four ulid tests covering length, monotonicity, uniqueness, and concurrent safety. The package documents the entropy source as math/rand-seeded-from-crypto/rand and explicitly warns against using ULIDs as session tokens; the doc keeps a future caller from reaching for `NewULID()` for an auth ticket.

### Cross-feature confirmation

`apps/server/main.go:42-58` shows the SEC-14 wiring path is live: `CHAT_DB_PATH=/tmp/x.db` causes `appdb.Open` to call `EnsureFile` (creates with 0600), then `appdb.Apply` to migrate. `apps/server/main.go:125-133` then constructs the channels + messages handlers and registers their routes — closing the chain between schema, ULID generation, and the user-facing API. The `phase-1/file-perms-and-headers` AC-1 finding is re-promoted from "function works in isolation" to "function is exercised at startup" (see file-perms-and-headers.md).

### Spec-vs-impl notes

- Spec lists implementation step 3 as `apps/server/internal/ids/ulid.go` and step 1 as `migrations/0001_init.sql` — both ship as named.
- `apps/server/internal/repo/repo.go` ships as a stub for downstream features; the channels and messages repo accessors land in the `feature-channels-and-messages` PR, not this one.

## Recommendations

1. No new tests added by this run — AC-4's previously-uncovered reading is now closed by the channels-and-messages handlers' INSERT sites, and the existing handler-level tests assert the persisted row's `id` matches the ULID shape `NewULID()` emits.
2. **Optional defensive test (low priority):** add a structural test that walks every `INSERT INTO {users,channels,messages}` site and asserts the ID column is bound to a value produced by `ids.NewULID()`. Catches the "someone added a CLI seed script that uses uuid" regression.
3. Spec follow-up (out of test-agent scope): clarify AC-4 wording (schema-permits vs. all-inserts-use-NewULID) so future implementers know which contract to honor and the test agent can pick the right anchor without re-deriving the load-bearing reading.
