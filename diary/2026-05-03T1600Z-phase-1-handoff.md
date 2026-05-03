# Phase 1 handoff

Date: 2026-05-03 16:00Z
Author: Claude (orchestrator session for phase-1 build-out)
Predecessor: `2026-05-03T1304Z-handoff.md` (phase-0 handoff at start of session)

This is the operational handoff for what landed in phase 1 (persistence + auth + size/rate caps + observability + cleanup) during the 2026-05-03 session. Reflective lessons live in the sibling entry `2026-05-03T1755Z-parallel-pr-churn-lessons.md`.

## What landed

### Persistence & schema
- **#26** — SQLite file created with `0600` perms (SEC-14); response security headers middleware (CSP, nosniff, no-referrer, frame-deny per SEC-10).
- **#29** — SQLite schema (`migrations/0001_init.sql`: users with `token_version` for US-12 server-side JWT revocation, channels, messages, auth_events). Migration runner + ULID generator (`internal/db`, `internal/ids`, `internal/repo` shell).

### Auth (the chain)
- **#33** — Auth internals: bcrypt + JWT primitives (`internal/auth`). Constant-time login with dummy-hash fallback for unknown user (SEC-3 timing); byte-identical error envelope for unknown-user vs wrong-password (SEC-4); JWT `tv` claim for server-side revocation (US-12).
- **#38** — Auth endpoints: `POST /api/register` (invite-code gated, `auth.EnforcePolicy`, bcrypt hash, JWT at `tv=0`), `POST /api/login`, `GET /api/me`, `POST /api/logout` (bumps `users.token_version`), `POST /api/ws-ticket` (30s single-use ticket, SEC-12). `RequireJWT` middleware. CLI accepts `--ws-ticket` and forwards as `?ticket=` on the WS upgrade URL.

### Hardening
- **#27** — Body + WS read/send caps. `httpx.BodyCap` middleware (REST 16 KiB, SEC-7 → 413). WS read limit 64 KiB (SEC-6 → close 1009). 4 KiB cap on decoded WS message body (SEC-8). Per-connection token bucket (10 msg/s, burst 30) closes flooders with close 1008.
- **#28** — Startup config validation (`internal/config`): refuses to boot on missing/short/denylisted/repeated/low-entropy/non-ASCII JWT secret (SEC-1); refuses non-loopback bind unless `CHAT_ALLOW_PUBLIC_BIND=1` (SEC-2); refuses to start without `CHAT_INVITE_CODE` while registration is enabled (US-11).

### Throttling
- **#41** — Per-IP rate limits: token-bucket fronted by bounded LRU. Login: burst 10 / 5 min (SEC-5 — the 11th login attempt within 5 min from one IP returns 429 + `Retry-After`). Register: burst 5 / 15 min. Per-username login backoff: linear, capped at `MaxDelay` (no lockout-DoS). Case-folded keys; records age out via `ResetAfter`. Audit row written on every 429 (`auth_events.kind = "rate_limited"`, SEC-13).

### WS upgrade hardening
- **#39** — Same-origin enforcement on `/ws` upgrade (`coder/websocket.Accept`'s built-in Host/Origin compare + `CHAT_ALLOWED_ORIGINS` allowlist). Ticket redemption on upgrade: `?ticket=<hex>` redeemed via `TicketStore.Redeem`; miss/expired/already-consumed returns HTTP 401 pre-handshake (RFC 6455 has no close codes before upgrade). `scripts/smoke.sh` mints a fresh ticket per WS dial.

### Channels & messages
- **#42** — `repo/channels.go`, `repo/messages.go`, `internal/http/channels_handlers.go`, `internal/http/messages_handlers.go`. `GET/POST /api/channels`, `GET/POST /api/channels/{id}/messages` (4 KiB body cap, ULID-cursor pagination, max 200/default 50). `wsapi.Handler` honors `?channel=<id>` (default `#general` for smoke). `POST /api/channels/{id}/messages` is the sole producer of WS broadcasts.

### Observability & error envelope
- **#26** + earlier — Access-log middleware strips `token` and `ticket` query params (SEC-11). User-safe error envelope shape `{ok, data, error}` per PRD §10.

### Lint + structural cleanup (cross-cutting)
- **#45** — Strict golangci-lint (errcheck, govet, staticcheck, unused, ineffassign, gocritic, gosec, revive, misspell, unparam, prealloc, bodyclose, noctx, nilerr, errorlint, exhaustive, gofmt, goimports) + ESLint v9 strict-type-checked + Prettier. CI `lint` job is required.
- **#51** — Post-phase-1 structural cleanup. 3-phase agent (planner → bullshit-review → impl). Consolidated duplicate envelope (`internal/httpx` deleted; `internal/http.errors.go` is the survivor with #38's status-first signature). 13 lint findings exposed by #45 against #38/#41/#42 code fixed in `a662351`. Plan + bullshit review committed as `specs/cleanup/post-phase1.md` for audit.

## PR / agent map

| PR  | Feature                            | Agent                  | Notes                                   |
|-----|-----------------------------------|------------------------|-----------------------------------------|
| #26 | file-perms + headers              | (pre-session)          | merged before this session               |
| #27 | body-and-ws-caps                  | wave-1 phase-1 agent   |                                         |
| #28 | startup-config-checks             | wave-1 phase-1 agent   | rebased once post-#33                   |
| #29 | sqlite-schema-and-ulid            | wave-1 phase-1 agent   |                                         |
| #33 | auth-internals                    | wave-1 phase-1 agent   | parent of #38 chain                     |
| #36 | cli-send-watch race fix (focused) | orchestrator follow-up | conflicted with #38's drive-by; #36 won |
| #38 | auth-endpoints                    | wave-2 phase-1 agent   | stacked on #33; cascaded rebases on #39/#41/#42 when squashed |
| #39 | ws-hardening                      | wave-2 phase-1 agent   | stacked on #38                           |
| #41 | rate-limits                       | wave-2 phase-1 agent   | stacked on #38                           |
| #42 | channels-and-messages             | wave-2 phase-1 agent   | stacked on #38                           |
| #45 | strict linters + CI gate          | linter agent           | `chore/strict-linters` off main          |
| #46 | integration anchor (do-not-merge) | orchestrator           | `integration/phase-1-all`; dies when #51 merges |
| #51 | post-phase-1 structural cleanup   | cleanup agent (3-phase)| stacked on #46                           |
| (this) | diary handoff                  | orchestrator           | stacked on #51                           |

## Known gaps carried into phase 2

- **Channel-validation on WS frames** (`{type:"error", code:"CHANNEL_NOT_FOUND"}`): NOT implemented in #39. Needs the typed inbound WS frame contract, which doesn't exist yet — phase-1 ships only the `?channel=<id>` query-param subscription form per the feature-spec allowance. Hook is the `readLoop` in `apps/server/internal/wsapi/handler.go`. Flagged in `specs/plans/phase-1/feature-ws-hardening.md` Implementation notes.
- **Per-conn user metadata on the WS hub**: `userID` is captured then discarded (`_ = userID`) in `wsapi/handler.go` because the hub has no per-conn metadata struct in phase-0/1. Will need a hub-level change for phase-2 presence. Also flagged in the cleanup PR's `specs/cleanup/post-phase1.md` deviations section.
- **`/api/auth/...` vs `/api/...` prefix**: PRD §10 names auth routes under `/api/auth/...` but #38 and channels-and-messages mount them at `/api/...`. Carried over as a known gap; if PRD-shape is wanted, it's a 1-commit rename fight.
- **`auth_events` column shape**: PRD §9 sketches `(id, user_id, username, event, source_ip, user_agent, created_at)`; the on-disk migration shipped `(id, user_id, kind, ip, ua, at)`. Endpoints log against the migration shape. If PRD-shaped is wanted, the right place is a follow-up migration in `migrations/`.
- **#28 initially broke `tests/server-ws-hub/` integration tests** because startup config validation refused to boot without `CHAT_JWT_SECRET` + `CHAT_INVITE_CODE`. Fixed in the same PR by adding env vars to the test's `cmd.Env`. Memory `feedback_ci_must_be_green.md` exists to prevent recurrence: agents must run `go test -race ./...` (not just their changed package) before declaring done.
- **Stale `apps/server/internal/config/` directory** sat untracked in the orchestrator's main checkout for a few hours after the linter agent's worktree corrupted state; moved to `/tmp/stale-config-backup-*` when surfaced. Not a code concern; orchestration sloppiness.

## Linter findings the cleanup PR closed

`a662351` (commit on #51) fixed 13 golangci-lint findings exposed by #45 against the freshly-merged #38/#41/#42 code:

- `gocritic exitAfterDefer` in `apps/server/main.go` (deferred cleanup wouldn't run after `log.Fatalf`)
- `gofmt` in `apps/server/main.go` and `apps/server/internal/http/auth_handlers.go`
- `gosec G101` in `apps/server/main.go` (env var name flagged as credential — silenced with reason)
- `revive exported` doc-comment requirements on `apps/server/internal/http/auth_store.go` (`ErrUsernameTaken`, `CreateUser`, etc.)

The same `gosec G101` false positive bit `apps/server/internal/config/config.go:15` (`EnvJWTSecret = "CHAT_JWT_SECRET"`) and was silenced with a reason comment + block doc comment in #45's branch (`62aebec`).

## Cleanup PR's plan + bullshit review

The cleanup agent's full plan + bull-review annotations are committed at `specs/cleanup/post-phase1.md` (173 lines) on PR #51. Audit there for plan-vs-implementation correspondence — every Phase A item maps to an atomic commit; B.1 / B.7 / B.8 were dropped in bull review with reasons; B.4 / B.6 dropped at impl time because the trigger thresholds didn't fire.

## Next phase entry point

Phase 2 plans live in `specs/plans/phase-2-*.md` (TUI + Web UI). The Phase 1 plan checklist (`specs/plans/phase-1-persistence-auth.md`) is fully ticked once #51 merges. Recommended phase-2 starting practices, captured today in `CLAUDE.md`'s "Parallel work" section:

1. Don't stack PRs on open PRs — every feature off `main`.
2. Refactor `apps/server/main.go` into a `routes.All()` loop + per-feature `internal/routes/<feature>.go` files **before** spawning parallel agents.
3. Move `CHANGELOG.md` to per-PR fragments under `CHANGELOG.d/`.
4. Write a 1-page contract (envelope shape, error codes, package boundaries, naming) before parallel feature work.
5. Linter is PR #0 of the phase.
6. Drive-by fixes in their own PR.

The reflective version of these is in the sibling diary entry `2026-05-03T1755Z-parallel-pr-churn-lessons.md` (already on main).
