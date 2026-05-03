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
**Last updated:** 2026-05-03T15:51:00Z
**Analyzed commit:** `e46dd51`

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

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred.

**Phase-1 totals (so far):** 8 features analyzed of 12 spec'd · 38 ACs · 27 covered · 3 partial · 0 missing · 8 deferred.

`feature-auth-endpoints` (PR #38) ships clean with 25+ in-package tests across the 5 endpoints + ticket store + middleware + auth-events recording. `scripts/smoke.sh` drives register → login → ws-ticket → watch and exits 0 against the live binary. The signing-key wiring is *behaviorally* sound (`config.Validate` enforces the strength rules at startup, then the handler reads `CHAT_JWT_SECRET` independently) but `apps/server/main.go` does not thread `cfg.JWTSecret` directly into `NewAuthHandlers.SigningKey` — the env var is read twice. The `feature-auth-internals` AC-5 partial flag should stay until that chain is concrete; see `auth-endpoints.md` cross-feature note.

Notable phase-1 gaps:
- `auth-internals` AC-5 partial: behaviorally satisfied but main.go reads `CHAT_JWT_SECRET` directly twice instead of threading `cfg.JWTSecret` through; AC-5 stays `partial` on a strict reading of "loaded from config".
- `logging-and-error-envelope` AC-1 partial: access-log line missing `IP` field — the new `access-log-fields-and-wiring` stub spec exists to close it. When the impl PR lands, AC-1 should re-promote.
- `sqlite-schema-and-ulid` AC-4 partial: schema permits ULIDs and `ids.NewULID()` is solid, but no shipped INSERT code path used it at the analyzed SHA — closed once `feature-channels-and-messages` (#42) lands.
- `security-headers-and-sqlite-ensure-wiring` and `access-log-fields-and-wiring`: companion stub specs covering the middleware-chain wiring in `apps/server/main.go`. Both are deferred until the implementation PR lands.

`feature-body-and-ws-caps` (PR #27) ships clean: WS read limit (64 KiB → close 1009), body cap (4 KiB), per-conn token bucket (close 1008), and REST 16 KiB cap (413). All four ACs covered by package-level tests with explicit library-constant guards.

**Phase-1 sibling PRs in flight (not yet on main):** PR #37 tracks `file-perms-and-headers` (1/3, SecurityHeaders not wired — superseded by the new wiring spec).

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
