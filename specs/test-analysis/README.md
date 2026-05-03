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
**Last updated:** 2026-05-03T17:26:50Z
**Analyzed commit:** `fa60bfd`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
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

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred.

**Phase-1 totals:** 14 features analyzed of 14 spec'd · 64 ACs · 63 covered · 1 partial · 0 missing · 0 deferred.

**Coordinated follow-up batch landed in `fa60bfd`:** the four planned-only stub specs (gap-A `access-log-fields-and-wiring`, gap-B `security-headers-and-sqlite-ensure-wiring`, gap-C `auth-endpoint-paths-align-with-prd`, gap-D `ws-userid-binding-and-channel-existence-check`) all closed in one PR set. Each one was tracked here at 0/N deferred; all four re-promote to N/N implemented at this SHA. The closure also transitively re-promotes three parent features whose ACs depended on the same wiring chain:

- `logging-and-error-envelope` AC-1 (was partial: missing `remote_ip` + `user_id`) → covered by `middleware.go:103`'s extended Printf format from gap-A.
- `file-perms-and-headers` AC-2 + AC-3 (was partial: `SecurityHeaders` defined but not on the live mux) → covered by `main.go:154`'s outermost `SecurityHeaders` wrap from gap-B.
- `ws-hardening` AC-3 + AC-4 (was partial + deferred: `_ = userID` discard + no channel-existence check) → covered by `connState{userID, channel}` + pre-upgrade-404 path from gap-D. AC-4 is now anchored on the pre-upgrade HTTP 404 + envelope (the originally-promised typed-frame variant stays explicitly out of scope per the gap-D spec).
- `channels-and-messages` AC-1 through AC-5 (were partial: handlers tested via httptest but routes never registered on the live mux) → covered by `main.go:133`'s `ch.Routes(mux, require, msg)` call.
- `sqlite-schema-and-ulid` AC-4 (was partial: schema permits ULIDs but no shipped INSERT site used `NewULID()`) → covered by the now-live `channels_handlers.go:77` and `messages_handlers.go:138` INSERT call sites.

**Remaining phase-1 gap (one AC):**
- `auth-internals` AC-5 partial: behaviorally satisfied but `apps/server/main.go` reads `CHAT_JWT_SECRET` directly twice (in `config.Validate` and in `NewAuthHandlers.SigningKey`) instead of threading `cfg.JWTSecret` through to the handler. AC-5 stays `partial` on a strict reading of "loaded from config" until the cfg→handler chain is concrete.

`feature-rate-limits` (PR #41) ships clean: per-IP token-bucket on `/api/auth/login` (10/5min) and `/api/auth/register` (5/15min) with bounded LRU; per-username linear backoff (2 free → 500ms steps capped at 2s, 5min idle eviction, case-insensitive); 429 envelope + RFC-7231 `Retry-After`; rejection rows in `auth_events`. 17 tests across the three test files. (Note the `/api/auth/<verb>` paths after gap-C's path-alignment closure.)

`feature-auth-endpoints` (PR #38, paths aligned by gap-C) ships clean with 25+ in-package tests across the 5 endpoints + ticket store + middleware + auth-events recording. `scripts/smoke.sh` drives register → login → ws-ticket → watch and exits 0 against the live binary.

**Phase-1 sibling PRs in flight (not yet on main):** none — phase-1 closes here modulo the single `auth-internals` AC-5 partial flag.

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
