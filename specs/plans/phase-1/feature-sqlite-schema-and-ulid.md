# Feature: SQLite schema and ULID generation

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- (foundational data model for US-1 through US-6, US-11, US-12)

## Acceptance criteria
- `migrations/0001_init.sql` defines tables for users, channels, messages, and `auth_events`.
- The `users` table includes a `token_version` column (used by US-12 logout-invalidation).
- A migration runner applies pending migrations on server startup.
- ULIDs are used as primary keys for users, channels, and messages.
- A package exposes a `NewULID()` helper (monotonic where possible).

## Implementation steps
1. Author `migrations/0001_init.sql` with:
   - `users(id TEXT PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, token_version INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMP NOT NULL)`
   - `channels(id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL, created_at TIMESTAMP NOT NULL)`
   - `messages(id TEXT PRIMARY KEY, channel_id TEXT NOT NULL REFERENCES channels(id), user_id TEXT NOT NULL REFERENCES users(id), body TEXT NOT NULL, created_at TIMESTAMP NOT NULL)`
   - `auth_events(id INTEGER PRIMARY KEY AUTOINCREMENT, user_id TEXT, kind TEXT NOT NULL, ip TEXT, ua TEXT, at TIMESTAMP NOT NULL)`
   - Indexes on `messages(channel_id, created_at)` and `auth_events(user_id, at)`.
2. Add a small migration runner (`apps/server/internal/db/migrate.go`) that records applied migrations in a `schema_migrations` table.
3. Add `apps/server/internal/ids/ulid.go` wrapping `github.com/oklog/ulid/v2` with a thread-safe monotonic source.
4. Wire migration application into server startup before accepting connections.

## Test plan
- Unit test: migration runner applies migrations to an in-memory SQLite DB and is idempotent on re-run.
- Unit test: `NewULID()` returns lexicographically increasing IDs across rapid calls.

## Files expected to be touched or created
- `migrations/0001_init.sql`
- `apps/server/internal/db/migrate.go`
- `apps/server/internal/db/migrate_test.go`
- `apps/server/internal/ids/ulid.go`
- `apps/server/internal/ids/ulid_test.go`

## Risks
- Schema changes after this point require new migrations; mitigated by getting the initial shape right before downstream features depend on it.
