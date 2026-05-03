# Test analysis

This directory is owned by the test-analysis agent (see `.claude/skills/test-watch/SKILL.md`).

Each file under `<phase>/<feature-slug>.md` reports the agent's coverage analysis for one feature spec in `specs/plans/`. The agent regenerates these on every run when there is a new commit on `main`.

## Layout

```
specs/test-analysis/
├── README.md                       # this index
└── <phase>/
    └── <feature-slug>.md           # per-feature findings (generated)
```

## How to run the agent

In a Claude Code session at the repo root:

```
/loop 90s /test-watch
```

The first tick after the loop starts will create `.claude/worktrees/test-agent` and `.claude/test-agent/state.json`, then check whether there's new work. Most ticks are silent no-ops. When new commits land on `main`, the agent opens a PR with new tests + an updated set of findings.

## How to run the analysis once, manually

```
/test-analyze
```

writes findings to this directory without creating a branch or opening a PR. Useful for ad-hoc inspection. Pair with `/test-implement` to write the tests, then a normal `git commit` + `gh pr create` if you want to ship the analysis yourself.

## Index

Generated automatically — leave this section alone; the agent rewrites it.

<!-- AGENT-INDEX-BEGIN -->
**Last updated:** 2026-05-03T20:38:00Z
**Analyzed commit:** `a283ba3`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 3/4 | 1 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [body-and-ws-caps](phase-1/body-and-ws-caps.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [logging-and-error-envelope](phase-1/logging-and-error-envelope.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [sqlite-schema-and-ulid](phase-1/sqlite-schema-and-ulid.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [auth-internals](phase-1/auth-internals.md) | implemented | 4/5 | 1 | 0 | 0 |
| phase-1 | [security-headers-and-sqlite-ensure-wiring](phase-1/security-headers-and-sqlite-ensure-wiring.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [startup-config-checks](phase-1/startup-config-checks.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [auth-endpoints](phase-1/auth-endpoints.md) | implemented | 7/7 | 0 | 0 | 0 |
| phase-1 | [access-log-fields-and-wiring](phase-1/access-log-fields-and-wiring.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [rate-limits](phase-1/rate-limits.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [auth-endpoint-paths-align-with-prd](phase-1/auth-endpoint-paths-align-with-prd.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [channels-and-messages](phase-1/channels-and-messages.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-1 | [ws-hardening](phase-1/ws-hardening.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [ws-userid-binding-and-channel-existence-check](phase-1/ws-userid-binding-and-channel-existence-check.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [file-perms-and-headers](phase-1/file-perms-and-headers.md) | implemented | 3/3 | 0 | 0 | 0 |
| phase-2 | [go-client-package](phase-2/go-client-package.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-2 | [ts-api-client-package](phase-2/ts-api-client-package.md) | implemented | 6/7 | 1 | 0 | 0 |
| phase-2 | [presence](phase-2/presence.md) | implemented | 4/5 | 0 | 0 | 1 |

**Phase-0 totals:** 4 features · 20 ACs · 19 covered · 1 partial · 0 missing · 0 deferred. The partial is `cli-send-watch` AC-1 — `chatd send` writes a frame the server now drops post-audit-#78. CLI-side tests still pass; system-level "send delivers a message that watchers see" is broken until `chatd send` is rewritten to POST against `/api/channels/{id}/messages` (lands with `20-feature-cli-full-commands`) or deprecated.

**Phase-1 totals:** 14 features analyzed of 14 spec'd · 64 ACs · 63 covered · 1 partial · 0 missing · 0 deferred.

**Phase-2 totals (so far):** 3 features analyzed of 5 spec'd (10/30/50 specs implemented; 20/40 unstarted) · 16 ACs · 14 covered · 1 partial · 0 missing · 1 deferred. AC-4 of `presence` (web app + CLI consumers) deferred until `40-feature-web-app` and a CLI presence consumer ship.

**Coordinated follow-up batch landed in `fa60bfd`:** the four planned-only stub specs (gap-A `access-log-fields-and-wiring`, gap-B `security-headers-and-sqlite-ensure-wiring`, gap-C `auth-endpoint-paths-align-with-prd`, gap-D `ws-userid-binding-and-channel-existence-check`) all closed in one PR set. Each one was tracked at 0/N deferred; all four re-promote to N/N implemented at this SHA. The closure also transitively re-promotes parent features whose ACs depended on the same wiring chain:

- `logging-and-error-envelope` AC-1 (was partial: missing `remote_ip` + `user_id`) → covered by `middleware.go:103`'s extended Printf format from gap-A.
- `file-perms-and-headers` AC-2 + AC-3 (was partial: `SecurityHeaders` defined but not on the live mux) → covered by `main.go:154`'s outermost `SecurityHeaders` wrap from gap-B.
- `ws-hardening` AC-3 + AC-4 (was partial + deferred: `_ = userID` discard + no channel-existence check) → covered by `connState{userID, channel}` + pre-upgrade-404 path from gap-D. AC-4 is now anchored on the pre-upgrade HTTP 404 + envelope (the originally-promised typed-frame variant stays explicitly out of scope per the gap-D spec).
- `channels-and-messages` AC-1 through AC-5 (were partial: handlers tested via httptest but routes never registered on the live mux) → covered by `main.go:133`'s `ch.Routes(mux, require, msg)` call.
- `sqlite-schema-and-ulid` AC-4 (was partial: schema permits ULIDs but no shipped INSERT site used `NewULID()`) → covered by the now-live `channels_handlers.go:77` and `messages_handlers.go:138` INSERT call sites.

**Remaining phase-1 gap (one AC):**
- `auth-internals` AC-5 partial: behaviorally satisfied but `apps/server/main.go` reads `CHAT_JWT_SECRET` directly twice (in `config.Validate` and in `NewAuthHandlers.SigningKey`) instead of threading `cfg.JWTSecret` through to the handler. AC-5 stays `partial` on a strict reading of "loaded from config" until the cfg→handler chain is concrete.

`feature-rate-limits` (PR #41) ships clean: per-IP token-bucket on `/api/auth/login` (10/5min) and `/api/auth/register` (5/15min) with bounded LRU; per-username linear backoff (2 free → 500ms steps capped at 2s, 5min idle eviction, case-insensitive); 429 envelope + RFC-7231 `Retry-After`; rejection rows in `auth_events`. 17 tests across the three test files. (Note the `/api/auth/<verb>` paths after gap-C's path-alignment closure.)

`feature-auth-endpoints` (PR #38, paths aligned by gap-C) ships clean with 25+ in-package tests across the 5 endpoints + ticket store + middleware + auth-events recording. `scripts/smoke.sh` drives register → login → ws-ticket → watch and exits 0 against the live binary.

`feature-go-client-package` (PR #64, with follow-ups #76 + 8ed4e82 + a4a5980) ships the Go HTTP+WS client at `packages/go-client/` (in-module, no `go.mod` of its own per CLAUDE.md). All 10 spec'd methods present; 23 tests across 5 `*_test.go` files cover REST + ticket-redeemed `Watch` end-to-end. AC-4 ("consumable from apps/cli via in-module import") satisfied at the import-graph level — no actual call site yet because that's `20-feature-cli-full-commands`'s job.

`feature-ts-api-client-package` (PR #67, with prettier follow-up #f85f955) ships `packages/api-client/` as `@hackathon/api-client` in the pnpm workspace. `Client` + `WebSocketClient` (with reconnect + `setTimeout` injection for deterministic tests) + `watch` async-iterable; 22 vitest cases. AC-6 (presence-event surface) sits at partial because the wire format is the client's *guess* at what `50-feature-presence` will emit — re-evaluate when that feature lands. AC-7 (consumable by apps/web) is satisfied at the workspace-graph level; `apps/web` doesn't exist yet (40-feature-web-app unstarted).

`feature-presence` (PR #80, server-side only) ships the hub-level refcount + WS broadcast + `GET /api/presence` REST endpoint. 17 in-package tests across `hub/presence_test.go` (9), `wsapi/presence_test.go` (4), `http/presence_handlers_test.go` (4). AC-4 (web app + CLI consumers) deferred until those features ship.

**Schema-drift flag (cross-feature):** the server emits the presence frame as `{type:"presence", data:{kind, user_id}}` but the TS api-client defines `PresenceEvent.data` as `{kind, user:User}` (full user record). One side needs to move — picking the lighter-weight `{user_id}` shape requires the client to maintain a userID→username map; embedding the full `User` matches the REST endpoint's `{id, username}` shape. **Production change required either way; out of test-agent scope.** Worth flagging when reviewing the next presence-client integration PR; the ts-api-client AC-6 partial should re-evaluate once both sides converge.

**Remaining phase 2 (2 features) and phase 3 (5 features):** `phase-2/{20,40}-feature-*.md` and all of `phase-3/*` not yet started; the agent will pick them up once their implementation commits land on `main`.

## Audit #78 response (`a283ba3`)

A security audit of the WS path landed in three coordinated PRs:

- **PR #85 (`92d447f`) `fix(wsapi): drop raw inbound WS rebroadcast`.** The phase-0 readLoop rebroadcast inbound frames verbatim, letting any authenticated peer forge `{type, data}` envelopes with arbitrary `sender_user_id` and impersonate other users — no DB write, no audit row. Fix: drop the broadcast; keep the read (drains the buffer; enforces size + rate limits). Producers must use `POST /api/channels/{id}/messages`. **Cross-impact:** `phase-0/server-ws-hub` AC-3 reframed (test was re-flipped to assert inbound-dropped + REST-broadcast); `phase-0/cli-send-watch` AC-1 silently functionally regressed (CLI test still passes; messages don't reach watchers).
- **PR #87 (`80c1de0`) `fix(wsapi): gate /debug/subs to loopback`.** The `internal-only` wording in `phase-0/server-ws-hub` AC-6 is now enforced rather than implicit — non-loopback sources get 403. New tests in `wsapi/debug_handler_test.go` cover the gate.
- **PR #86 (`cb1e075`) `fix(audit): access log records authenticated user_id`.** The auth middleware wrote user_id under `auth.ctxKeyUserID`; the access log read via `http.UserID` (different unexported key) — so `user_id` was always `-` for authenticated requests in production. The in-isolation middleware tests passed because they wrote+read through the same key. Fix: pointer sink installed by AccessLog, written through by RequireJWT via a new `MiddlewareConfig.WithUserID` callback. New chain-level black-box test added. **Generalizable lesson:** middleware tests must drive the production chain order to catch keying mismatches.

**Remaining gap from audit follow-up:** `cli-send-watch` AC-1 is partial. Closing it is a production change (rewrite `chatd send` to POST against the messages endpoint, requires CLI auth wiring) — natural home is `20-feature-cli-full-commands`.
<!-- AGENT-INDEX-END -->
