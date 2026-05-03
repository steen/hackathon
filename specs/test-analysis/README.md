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
**Last updated:** 2026-05-03T19:55:35Z
**Analyzed commit:** `ff5576d`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [body-and-ws-caps](phase-1/body-and-ws-caps.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [logging-and-error-envelope](phase-1/logging-and-error-envelope.md) | partial | 3/4 | 1 | 0 | 0 |
| phase-1 | [sqlite-schema-and-ulid](phase-1/sqlite-schema-and-ulid.md) | implemented | 4/5 | 1 | 0 | 0 |
| phase-1 | [auth-internals](phase-1/auth-internals.md) | implemented | 4/5 | 1 | 0 | 0 |
| phase-1 | [security-headers-and-sqlite-ensure-wiring](phase-1/security-headers-and-sqlite-ensure-wiring.md) | stub | 0/4 | 0 | 0 | 4 |
| phase-1 | [startup-config-checks](phase-1/startup-config-checks.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [auth-endpoints](phase-1/auth-endpoints.md) | implemented | 7/7 | 0 | 0 | 0 |
| phase-1 | [access-log-fields-and-wiring](phase-1/access-log-fields-and-wiring.md) | stub | 0/4 | 0 | 0 | 4 |
| phase-1 | [rate-limits](phase-1/rate-limits.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [auth-endpoint-paths-align-with-prd](phase-1/auth-endpoint-paths-align-with-prd.md) | stub | 0/4 | 0 | 0 | 4 |
| phase-1 | [channels-and-messages](phase-1/channels-and-messages.md) | partial | 1/6 | 5 | 0 | 0 |
| phase-1 | [ws-hardening](phase-1/ws-hardening.md) | partial | 2/4 | 1 | 0 | 1 |
| phase-1 | [ws-userid-binding-and-channel-existence-check](phase-1/ws-userid-binding-and-channel-existence-check.md) | stub | 0/5 | 0 | 0 | 5 |
| phase-1 | [file-perms-and-headers](phase-1/file-perms-and-headers.md) | partial | 1/3 | 2 | 0 | 0 |
| phase-2 | [go-client-package](phase-2/go-client-package.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-2 | [ts-api-client-package](phase-2/ts-api-client-package.md) | implemented | 6/7 | 1 | 0 | 0 |

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred.

**Phase-1 totals (on origin/main at this SHA):** 14 features analyzed of 14 spec'd · 64 ACs · 35 covered · 11 partial · 0 missing · 18 deferred. **Sibling PR #79 (open against `fa60bfd`) re-promotes phase-1 to 63/64 covered + 1 partial + 0 deferred** by closing the gap-A/B/C/D stub-closure batch — once PR #79 merges, those rows in this index will reflect the new totals.

**Phase-2 totals (so far):** 2 features analyzed of 5 spec'd (10/30 specs implemented; 20/40/50 unstarted) · 11 ACs · 10 covered · 1 partial · 0 missing · 0 deferred.

`feature-go-client-package` (PR #64, with follow-ups #76 + 8ed4e82 + a4a5980) ships the Go HTTP+WS client at `packages/go-client/` (in-module, no `go.mod` of its own per CLAUDE.md). All 10 spec'd methods present; 23 tests across 5 `*_test.go` files cover REST + ticket-redeemed `Watch` end-to-end. AC-4 ("consumable from apps/cli via in-module import") satisfied at the import-graph level — no actual call site yet because that's `20-feature-cli-full-commands`'s job.

`feature-ts-api-client-package` (PR #67, with prettier follow-up #f85f955) ships `packages/api-client/` as `@hackathon/api-client` in the pnpm workspace. `Client` + `WebSocketClient` (with reconnect + `setTimeout` injection for deterministic tests) + `watch` async-iterable; 22 vitest cases. AC-6 (presence-event surface) sits at partial because the wire format is the client's *guess* at what `50-feature-presence` will emit — re-evaluate when that feature lands. AC-7 (consumable by apps/web) is satisfied at the workspace-graph level; `apps/web` doesn't exist yet (40-feature-web-app unstarted).

`feature-ws-userid-binding-and-channel-existence-check` (new in PR #79) is the third planned-only follow-up stub spec, exists to close `feature-ws-hardening` AC-3 (partial) and AC-4 (deferred). Reframes AC-4 as a pre-upgrade HTTP 404 + envelope. The impl landed in `fa60bfd`; PR #79 re-promotes it.

`feature-auth-endpoints` (PR #38) ships clean with 25+ in-package tests across the 5 endpoints + ticket store + middleware + auth-events recording. `scripts/smoke.sh` drives register → login → ws-ticket → watch and exits 0 against the live binary. The signing-key wiring is *behaviorally* sound (`config.Validate` enforces the strength rules at startup, then the handler reads `CHAT_JWT_SECRET` independently) but `apps/server/main.go` does not thread `cfg.JWTSecret` directly into `NewAuthHandlers.SigningKey` — the env var is read twice. The `feature-auth-internals` AC-5 partial flag should stay until that chain is concrete; see `auth-endpoints.md` cross-feature note.

Notable phase-1 gaps (snapshot at `ff5576d`; PR #79 closes most of these):
- `auth-internals` AC-5 partial: behaviorally satisfied but main.go reads `CHAT_JWT_SECRET` directly twice instead of threading `cfg.JWTSecret` through; AC-5 stays `partial` on a strict reading of "loaded from config".
- `logging-and-error-envelope` AC-1 partial: access-log line missing `IP` field — closed by gap-A in `fa60bfd` (`access-log-fields-and-wiring`); PR #79 re-promotes.
- `sqlite-schema-and-ulid` AC-4 partial: schema permits ULIDs and `ids.NewULID()` is solid; the INSERT call sites in `channels_handlers.go:77` and `messages_handlers.go:138` close the contract — PR #79 re-promotes.
- `channels-and-messages` AC-1 through AC-5 partial: handler unit tests pass but routes weren't on the live mux at `000a530`. **Wiring gap closed by PR #42 on main** (`ch.Routes(mux, require, msg)` at `apps/server/main.go:133`); PR #79 re-promotes.
- `ws-hardening` AC-3 partial + AC-4 deferred: handler stashed userID in `_ = userID` with a TODO. Closed by gap-D (`ws-userid-binding-and-channel-existence-check`) in `fa60bfd`; PR #79 re-promotes both — AC-3 to covered, AC-4 reframed as pre-upgrade HTTP 404 + envelope.
- `security-headers-and-sqlite-ensure-wiring`, `access-log-fields-and-wiring`, `auth-endpoint-paths-align-with-prd`, `ws-userid-binding-and-channel-existence-check`: stub specs tracking unimplemented follow-ups at this SHA; all four impls landed in `fa60bfd` and PR #79 re-promotes them.

`feature-rate-limits` (PR #41) ships clean: per-IP token-bucket on `/api/auth/login` (10/5min) and `/api/auth/register` (5/15min) with bounded LRU; per-username linear backoff (2 free → 500ms steps capped at 2s, 5min idle eviction, case-insensitive); 429 envelope + RFC-7231 `Retry-After`; rejection rows in `auth_events`. 17 tests across the three test files.

**Phase-1/2 sibling PRs in flight (not yet on main):** PR #79 (phase-1 stub-closure batch + 5 parent re-promotions). When it merges, the phase-1 rows in this index re-promote to 63/64 covered + 1 partial.

**Phases 2 (remaining) and 3:** `phase-2/{20,40,50}-feature-*.md` and all of `phase-3/*` not yet started; the agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
