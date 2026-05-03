# Changelog

All notable changes to Discord Lite are recorded here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) (with timestamped sections instead of `[Unreleased]`) and the project uses [Semantic Versioning](https://semver.org/).

This changelog is intentionally **high-level**: meaningful product, architectural, or operational changes only — not every commit. Each merged PR with user-visible or operational impact gets its own timestamped section, newest first.

## Planned (next)
- Phase 0 — walking skeleton: server + two CLI clients exchanging real-time messages, validated by `scripts/smoke.sh`.
- Phase 1 — persistence, auth, full message envelope.
- Phase 2 — TUI and Web UI.
- Phase 3 — polish, requirement-coverage report, demo build.

## 2026-05-03 18:00Z — Rate limits: per-IP login/register, per-username login backoff (phase 1)

### Added
- `apps/server/internal/ratelimit/iplimit.go` — token-bucket limiter keyed by IP, fronted by a bounded LRU so an attacker rotating source IPs cannot grow memory (PRD §14 risk). `LoginIPConfig` (burst 10 / 5 min) and `RegisterIPConfig` (burst 5 / 15 min) match PRD §9. `LoginIPConfig` is the source of truth for SEC-5 (the 11th login attempt within 5 min from one IP returns 429).
- `apps/server/internal/ratelimit/userlimit.go` — per-username login-failure tracker with linear backoff (configurable Step + GraceFailures), capped at MaxDelay so a forgotten password cannot lock an account out (PRD §9: "without enabling lockout-DoS"). Username keys are case-folded so case-only variation cannot bypass the gate. Records age out after `ResetAfter`.
- `apps/server/internal/http/middleware_ratelimit.go` — `IPRateLimit` middleware that consumes one token per request, writes the user-safe envelope on 429 (new `CodeRateLimited` error code), sets `Retry-After`, and writes one `auth_events` row (kind `rate_limited`) so rejections are observable per the spec AC.
- `apps/server/internal/http/auth_handlers.go` — `Login` consults the per-username gate before authenticating, increments on failure, clears it on success. `AuthDeps` grows an optional `UserLimiter` so tests can opt in/out.
- `apps/server/main.go` — wires both IP limiters and the username limiter onto `/api/login` (5-minute Retry-After) and `/api/register` (15-minute Retry-After).
- Tests: `TestIPRateLimitBlocksEleventhLoginAttemptWithin5min` (SEC-5, name and assertion both pin the exact threshold), per-IP register burst limit, refill-over-time, LRU eviction, concurrent access, per-username backoff growth, MaxDelay cap, Reset on success, ResetAfter expiry, case-insensitivity, and the `auth_events` rate-limit audit row.

## 2026-05-03 17:30Z — Auth endpoints: register / login / me / logout / ws-ticket (phase 1)

### Added
- `apps/server/internal/http` — new package owning the JSON envelope helpers and the auth HTTP handlers (the package shadows `net/http` deliberately; call sites import it as `httpapi`):
  - `envelope.go`: `WriteOK` / `WriteError` and the PRD §10 `{ok, data, error}` envelope shape with a small enum of stable `error.code` values (`bad_request`, `unauthorized`, `forbidden`, `conflict`, `internal`, …).
  - `auth_handlers.go`: `POST /api/register` (invite-code gated, applies `auth.EnforcePolicy`, hashes via bcrypt, mints a JWT at `tv=0`), `POST /api/login` (delegates to `auth.AuthenticateLogin` so SEC-3/SEC-4 stay in one place), `GET /api/me`, `POST /api/logout` (bumps `users.token_version` to invalidate every previously-issued JWT — US-12), `POST /api/ws-ticket` (issues a 30s, single-use ticket bound to the caller — SEC-12). Every endpoint writes a row to `auth_events` (SEC-13).
  - `auth_store.go`: SQL helpers for `users` and `auth_events` written in this package (parameterized statements only, per PRD §6) since `internal/repo` is frozen by its parent PR.
  - Strict JSON decoding (`DisallowUnknownFields`) and a 16 KiB body cap via `http.MaxBytesReader` so a typo-bearing or oversized client request fails loud at the boundary instead of silently bypassing a required field.
- `apps/server/internal/auth/tickets.go` — in-memory `TicketStore` (`Issue` / `Redeem`) keyed on a 32-byte hex token; `Redeem` deletes before returning so a second call cannot succeed even under contention (SEC-12 race rider). 30s TTL; clock injectable for tests. Tests cover single-use semantics, expiry boundary, unknown-ticket, and 50-goroutine contention with exactly-one-winner.
- `apps/server/internal/auth/middleware.go` — `RequireJWT` extracts a `Bearer` token, performs a two-phase parse (subject extraction → DB lookup → re-parse against the row's current `token_version`) so a deleted-user token cannot be distinguished from a bad signature via status code, then decorates the request context with `user_id` + `username`. Companion `jwt_subject.go` carries the unverified-subject helper rather than touching the parent PR's `jwt.go`.
- `apps/server/main.go` — when `CHAT_DB_PATH` is set, wires the auth handlers + `RequireJWT` middleware onto the mux. `CHAT_JWT_SECRET` is required on this path (full secret-strength validation lands with the startup-checks feature).
- `apps/cli/main.go` + `apps/cli/cmd/url.go` — chatd accepts `--ws-ticket TICKET` and appends it as `?ticket=…` on the WebSocket URL. Forwarding only; the WS upgrader does not yet redeem the ticket (that's the ws-hardening feature). Wiring is in place so the smoke script can already pass tickets end-to-end.
- `scripts/smoke.sh` — now runs through `register → login → ws-ticket` against the real HTTP API (curl + python3 JSON parse, no jq dep), then hands the ticket to `chatd watch` / `chatd send`. The phase-0 fan-out assertion stays unchanged. Each smoke invocation is hermetic: `CHAT_DB_PATH` lives in the temp work dir, and `CHAT_JWT_SECRET` + `CHAT_INVITE_CODE` are scoped to the script.
- Tests covering US-1, US-2, US-11, US-12, SEC-3, SEC-4, SEC-12, SEC-13: register success / wrong invite / bad username / short password / duplicate; login success / wrong password / byte-identical envelope vs unknown-user; `/api/me` for valid + post-logout (US-12); `auth_events` row counts for register/login_success/login_failure/logout (SEC-13); ws-ticket single-use, expiry boundary, concurrent redemption (SEC-12).

### Fixed
- `tests/cli-send-watch/cli_test.go` — wrap the test buffer in a mutex (`safeBuffer`) so the polling read in the test does not race the `cmd.Watch` goroutine's `Fprintln`. Without this, `go test -race ./...` failed on this preexisting test even with no auth-endpoint changes touched.
- `apps/server/internal/auth/jwt_test.go` `TestJWTRejectsTamperedSignature` — flip a byte in the *middle* of the signature segment instead of the last char. The trailing char of an unpadded base64url 32-byte HMAC encodes only 4 useful bits; substituting it for another char with the same high-4-bits decoded to the same signature bytes and the assertion legitimately failed (~25% flake rate on `-count=20`).

### Notes / known gaps
- PRD §10 names the auth routes under `/api/auth/...`; the feature spec and this PR mount them at `/api/...`. Following the feature spec is intentional — flagged here so the channels/messages feature can revisit the prefix decision.
- PRD §9 describes an `auth_events` row as `(id, user_id, username, event, source_ip, user_agent, created_at)` but the on-disk migration (parent PR #29) shipped `(id, user_id, kind, ip, ua, at)`. The endpoints log against the migration shape per the explicit instruction in the feature spec; if PRD-shaped columns are wanted, a migration in the parent (sqlite-schema) feature is the right place.

## 2026-05-03 16:45Z — Auth internals: bcrypt + JWT + password policy (phase 1)

### Added
- `apps/server/internal/auth` — package holding the password and JWT primitives the auth endpoints will plug into:
  - `password.go`: `Hash`, `Verify`, `EnforcePolicy`, `VerifyDummy`. Verify collapses every failure mode (wrong password, malformed hash) into a single `ErrInvalidPassword` so callers cannot leak the failure arm. `EnforcePolicy` rejects passwords shorter than 10 bytes (PRD §9) and longer than 72 bytes (bcrypt input limit; rejecting beats silent truncation).
  - `jwt.go`: `Issue` and `Parse` for HS256 tokens with a `tv` (token-version) claim. `Parse` checks signature, issuer, expiry, and that the token's `tv` equals the user's current `token_version` — bumping the row's counter on logout invalidates every previously-issued JWT (US-12), no deny-list table needed.
  - `login.go`: `AuthenticateLogin(lookup, username, password)`. When the username is unknown, the code still runs bcrypt against a precomputed dummy hash so the response time stays in the same ballpark as a real wrong-password attempt and an attacker cannot enumerate accounts via timing (PRD §9, SEC-3). Both failure arms return the byte-identical `LoginErrorMessage` (SEC-4).
  - `constants.go`: policy thresholds, JWT TTL/issuer, and the precomputed dummy bcrypt hash. The hash was generated once with `bcrypt.GenerateFromPassword([]byte("never-matches"), bcrypt.DefaultCost)` and pasted as a const so package init does no work.
- Tests carry SEC-3 (timing within tolerance, sanity check) and SEC-4 (byte-identical error text) IDs where applicable.
- `go.mod` picks up `github.com/golang-jwt/jwt/v5` and `golang.org/x/crypto`.

## 2026-05-03 14:21Z — SQLite schema + ULID generation (phase 1)

### Added
- `migrations/0001_init.sql` — baseline schema for `users` (with `token_version` for US-12 server-side JWT revocation), `channels`, `messages`, and `auth_events`. Indexes on `messages(channel_id, created_at)` and `auth_events(user_id, at)` to support paginated history and audit queries.
- `migrations/embed.go` — `embed.FS` of every `*.sql` migration sibling, exposed as `migrations.FS`. The package lives under `migrations/` so `go:embed` (which cannot escape its own package directory) can reach the files at the canonical PRD §6 location.
- `apps/server/internal/db` — `Apply`/`ApplyFS` migration runner (records applied filenames in a `schema_migrations` table, idempotent on re-run, transactional per file) and `Open` helper that creates the SQLite file at `0600` (PRD §9) and opens via `modernc.org/sqlite` with WAL + FK-on pragmas.
- `apps/server/internal/ids` — `NewULID()` wrapping `oklog/ulid/v2` with a `LockedMonotonicReader` so concurrent callers stay strictly increasing within the same millisecond.
- `apps/server/internal/repo` — `repo.Repo` data-access façade with `New(*sql.DB)`. Concrete accessors land in later phase-1 features.
- `apps/server/main.go` now opens the DB and applies migrations before accepting connections when `CHAT_DB_PATH` is set. Gating on the env var keeps the phase-0 `scripts/smoke.sh` boot path file-free until later phase-1 features (auth, channels, messages) require persistence.

## 2026-05-03 15:30Z — chatd CLI binary entrypoint + smoke test (phase 0) (#18)

### Added
- `apps/cli/main.go` is now a `package main` dispatcher delegating to `apps/cli/cmd`. Supports `chatd [--url URL] send <msg...>` and `chatd [--url URL] watch`; URL falls back to `CHAT_SERVER` env or the default. Cancels on SIGINT/SIGTERM via `signal.NotifyContext`. The library shipped in #14; this lands the missing entrypoint.
- `scripts/smoke.sh` is the phase-0 system test: it builds `bin/server` and `bin/chatd`, picks a free port (via a one-shot `python3` socket bind, with `CHAT_SERVER_PORT` env override if `python3` is unavailable; the script errors out clearly if `python3` is missing and no override is set), boots the server, starts two `chatd watch` processes, sends a unique message via `chatd send`, and asserts both watchers received it within a 5s budget. Tears down all spawned processes via `trap cleanup EXIT INT TERM HUP` and dumps server/watcher logs only on failure.
- `pnpm smoke` shortcut and `pnpm test` (root) now runs the smoke script first, then the workspace test loop. Smoke first so a broken phase-0 system test fails fast before slower unit suites run, and so a missing workspace dep (e.g. unbuilt `tests/node_modules`) does not silently skip the canonical phase-0 acceptance test.
- CI: the `go` job in `.github/workflows/ci.yml` now runs `bash scripts/smoke.sh` after `go test`. The pnpm job uses `pnpm -r` which bypasses root scripts, so smoke is wired explicitly here rather than via `pnpm test`.

### Removed
- `apps/cli/doc.go` (its `package cli` declaration conflicted with the new `package main` in the same directory).

## 2026-05-03 15:20Z — Server `/ws` endpoint with in-memory hub (phase 0) (#17)

### Added
- `apps/server/main.go` boots an HTTP server on `CHAT_SERVER_PORT` (default `8080`, validated 1–65535) with `ReadHeaderTimeout`/`IdleTimeout` (Slowloris mitigation) and graceful shutdown on SIGINT/SIGTERM.
- `apps/server/internal/hub` provides per-channel pub/sub fan-out with `Subscribe` / `Unsubscribe` / `Broadcast`. Broadcast snapshots the subscriber set under a read lock so a slow subscriber cannot stall the hub.
- `apps/server/internal/wsapi` exposes the `/ws` HTTP handler. Default Origin verification is enforced (no `InsecureSkipVerify`). Each accepted connection auto-subscribes to `#general` for its lifetime; reads broadcast as text frames; writes drain through a 64-slot buffered queue (overflow drops messages for that subscriber rather than blocking the hub).

## 2026-05-03 13:05Z — CLI send/watch library (no-auth, phase 0) (#14)

### Added
- `apps/cli/cmd` package implementing `Send` (one-shot text frame to `/ws`) and `Watch` (read loop streaming text frames to an `io.Writer`) against a `coder/websocket` client; both perform a clean `StatusNormalClosure` handshake on parent-context cancellation.
- `ResolveURL` precedence: explicit flag → `CHAT_SERVER` env var → `ws://localhost:8080/ws`.
- AC-tagged test gate (`TestAC_0_4_NoAuthSymbolsReferencedFromCLI`) statically verifying no auth-related imports or literals leak into CLI sources during phase 0.

## 2026-05-03 11:59Z — Single-root Go module (#8)

### Changed
- Go module layout collapsed from `go.work` + per-app `go.mod` to a single root `go.mod` with module name `hackathon`. Imports use `hackathon/<path>`. The module name is intentionally decoupled from the GitHub coordinate so it survives org renames; the trade-off is that the module is not `go get`-able from outside the repo.

## 2026-05-03 11:45Z — Server WebSocket endpoint with in-memory hub (#6) — *later rolled back*

### Added
- `/ws` handler backed by a per-channel subscriber registry on `#general`, environment-driven config (`SERVER_PORT`, validated 1–65535), explicit `*http.Server` with `ReadHeaderTimeout` and `IdleTimeout` (Slowloris mitigation), clean WebSocket close (`StatusNormalClosure`).

### Removed
- The above merge was force-pushed off `main` shortly after landing; `apps/server/` reverted to its phase-0 stub. Functionality will land again as part of the next CLI/server PR.

## 2026-05-03 11:36Z — Monorepo scaffold (#5)

### Added
- `go.work` + `pnpm-workspace.yaml` + root `package.json` with `dev`/`build`/`test` fan-out scripts.
- GitHub Actions CI: per-module Go build/test, pnpm install/build/test, workflow-level concurrency group, least-privilege `contents: read` permissions.

## 2026-05-03 — Initial repository

### Added
- Initial Product Requirements Document at `specs/PRD.md`.
- Specifications live in a top-level `specs/` directory.
- Architectural decision: monorepo via pnpm workspaces (JS/TS) + a single root `go.mod` (Go).
- Architectural decision: web UI on Vite + React + TypeScript, chosen for first-class WebRTC support via LiveKit's React SDK.
- Architectural decision: SQLite for MVP with a documented PostgreSQL upgrade path (mirrored migrations, repository interfaces, dialect-portable SQL).
- Architectural decision: hub abstraction (in-proc today, NATS/Redis future) and `origin_server_id` on every message to prepare for federation.
- Architectural decision: opaque-payload message envelope (`payload`, `nonce`, `sender_key_id`, `recipient_wraps`) so the server cannot read message contents, enabling future E2E encryption without a server-side change.
- Architectural decision: ULIDs for all entity IDs.
- Testing stance: coverage is measured against requirements (user stories and functional requirements), not lines of code. Tests are tagged with requirement IDs.
- Repository initialized.
