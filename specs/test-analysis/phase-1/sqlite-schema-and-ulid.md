---
feature: sqlite-schema-and-ulid
phase: phase-1
analyzed_at: 2026-05-03T14:56:48Z
analyzed_commit: f765726b61246bcf8ecdf8ea33e6b7d0ff36847b
implementation_status: implemented
total_acs: 5
covered: 4
partial: 1
missing: 0
deferred: 0
---

# Test analysis: SQLite schema and ULID generation

**Spec:** `specs/plans/phase-1/feature-sqlite-schema-and-ulid.md`
**Implementation status:** implemented — `migrations/0001_init.sql` (embedded via `migrations/embed.go`) defines users/channels/messages/auth_events with the required columns and indexes. `apps/server/internal/db/{open,migrate}.go` open the DB (calling `EnsureFile` for SEC-14 0600 perms), apply migrations idempotently, and `apps/server/main.go` wires both into startup behind `CHAT_DB_PATH`. `apps/server/internal/ids/ulid.go` exposes `NewULID()` with a thread-safe monotonic source. A skeleton `apps/server/internal/repo` package exists for downstream features; no domain accessors yet.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `migrations/0001_init.sql` defines tables for users, channels, messages, and `auth_events`. | covered | `apps/server/internal/db/migrate_test.go::TestApplyAppliesEmbeddedMigrations` (queries `sqlite_master` for all four tables + `schema_migrations`). |
| AC-2 | The `users` table includes a `token_version` column (used by US-12 logout-invalidation). | covered | `apps/server/internal/db/migrate_test.go::TestApplyAppliesEmbeddedMigrations` (explicit `PRAGMA table_info(users)` walk asserts `token_version` is present). |
| AC-3 | A migration runner applies pending migrations on server startup. | covered | `apps/server/internal/db/migrate_test.go::TestApplyIsIdempotent` + `TestApplyFSAppliesPendingOnly` + `TestApplyRejectsNilDB` (runner is idempotent, applies pending-only, rejects nil DB). Wired into startup at `apps/server/main.go:48-53` behind `CHAT_DB_PATH`. |
| AC-4 | ULIDs are used as primary keys for users, channels, and messages. | partial | The schema declares `id TEXT PRIMARY KEY` for all three tables (correct storage type for ULID strings) and `ids.NewULID()` exists with strong tests. **No shipped code path actually inserts a row using `NewULID()`** — `repo.Repo` has no accessor methods yet. The convention is set up correctly; whether the next feature respects it is unverifiable today. |
| AC-5 | A package exposes a `NewULID()` helper (monotonic where possible). | covered | `apps/server/internal/ids/ulid_test.go::TestNewULIDLength` (26 chars, Crockford-base32) + `TestNewULIDIsLexicographicallyIncreasing` (10k strict-greater chain) + `TestNewULIDIsUnique` (10k no-collision) + `TestNewULIDConcurrentSafe` (32 goroutines × 1k under -race). |

## Findings

### Covered

- **AC-1, AC-2** — `TestApplyAppliesEmbeddedMigrations` checks both the table set and the specific column the spec calls out for US-12. The `IF NOT EXISTS` clauses in the SQL plus the `schema_migrations` ledger together make the runner idempotent at two layers (SQL-level + ledger-level), which the idempotency test verifies.
- **AC-3** — Three tests cover the runner: applies-once, applies-pending-only, rejects-nil. The startup wiring (`appdb.Apply(migCtx, sqlDB)` with a 30s timeout) is reachable when `CHAT_DB_PATH` is set; phase-0 boot path (`scripts/smoke.sh`) intentionally leaves the env var unset and gets the no-DB code path.
- **AC-5** — Four ulid tests covering length, monotonicity, uniqueness, and concurrent safety. The package documents the entropy source as math/rand-seeded-from-crypto/rand and explicitly warns against using ULIDs as session tokens; the doc keeps a future caller from reaching for `NewULID()` for an auth ticket.

### Partial — AC-4

The AC's literal text — "ULIDs are used as primary keys for users, channels, and messages" — has two reasonable readings:

1. **Schema permits ULIDs.** The `TEXT PRIMARY KEY` declaration in `0001_init.sql` is the canonical SQLite storage shape for a ULID string. AC-1's test indirectly covers this by asserting the tables exist with the spec-defined shape; if you read AC-4 this way, it's covered.
2. **All inserts call `NewULID()`.** A behavioral contract on the data-access layer. No INSERT code ships in this PR; `repo.Repo` only exposes `New(sqlDB)` and `DB()`. There is nothing to test against — and nothing is at risk of regressing — until `feature-channels-and-messages` and `feature-auth-internals` add the typed accessors.

I'm marking partial because (1) is satisfied today but (2) is not, and (2) is the load-bearing reading for downstream features. When typed accessors land, the next test-watch tick should re-promote this AC to covered iff each insert path uses `ids.NewULID()`. A static grep test (`grep -r 'INSERT INTO users\|channels\|messages' apps/server/internal/repo/` and verify each call site supplies an ID from `ids.NewULID()`) could catch a regression where someone uses `uuid.NewString()` or a counter — out of scope for this run since there's no insert code yet.

### Cross-feature confirmation

`apps/server/main.go:42-58` shows the SEC-14 wiring path is now live: `CHAT_DB_PATH=/tmp/x.db` causes `appdb.Open` to call `EnsureFile` (creates with 0600), then `appdb.Apply` to migrate. **This promotes `phase-1/file-perms-and-headers` AC-1 from "function works in isolation" to "function is exercised at startup"**; that finding doc should be re-evaluated on the next tick. (Not edited here to keep this PR focused on `feature-sqlite-schema-and-ulid`.)

### Spec-vs-impl notes

- Spec lists implementation step 3 as `apps/server/internal/ids/ulid.go` and step 1 as `migrations/0001_init.sql` — both ship as named.
- `apps/server/internal/repo/repo.go` is new in this PR but not in the spec's "Files expected" list. It's a stub for downstream features (next ticks will track its accessors as those features land); no AC currently requires it.

## Recommendations

1. No new tests added by this run — coverage is appropriate at the unit level and AC-4's load-bearing reading isn't verifiable until insert code ships.
2. Spec follow-up: clarify AC-4 wording (schema-permits vs. all-inserts-use-NewULID) so future implementers know which contract to honor and the test agent can pick the right anchor.
3. Once `feature-channels-and-messages` lands, add a structural test that walks every `INSERT INTO {users,channels,messages}` site and verifies the ID column is bound to a value produced by `ids.NewULID()`. This catches the "someone added a CLI seed script that uses uuid" regression.
4. Out of scope but worth flagging: `phase-1/file-perms-and-headers` finding will need a refresh — `EnsureFile` is now actively called on startup, not just unit-tested.
