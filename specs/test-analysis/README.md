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
**Last updated:** 2026-05-03T15:59:48Z
**Analyzed commit:** `000a530`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [body-and-ws-caps](phase-1/body-and-ws-caps.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-1 | [logging-and-error-envelope](phase-1/logging-and-error-envelope.md) | partial | 3/4 | 1 | 0 | 0 |
| phase-1 | [sqlite-schema-and-ulid](phase-1/sqlite-schema-and-ulid.md) | implemented | 4/5 | 1 | 0 | 0 |
| phase-1 | [channels-and-messages](phase-1/channels-and-messages.md) | partial | 1/6 | 5 | 0 | 0 |

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred.

**Phase-1 totals (so far):** 4 features analyzed of 12 spec'd · 19 ACs · 12 covered · 7 partial · 0 missing · 0 deferred. The 7 partials are:
- `logging-and-error-envelope` AC-1: access-log line missing `IP` (production gap; closes when `feature-access-log-fields-and-wiring` ships).
- `sqlite-schema-and-ulid` AC-4: schema permits ULIDs but no INSERT code path used `ids.NewULID()`. **Now closed at the contract level** — `feature-channels-and-messages` introduces both load-bearing INSERT call sites; the next tick should re-promote this AC to covered.
- `channels-and-messages` AC-1 through AC-5: handlers + repo + WS broadcast all ship with strong unit + integration tests, **but main.go does NOT mount `/api/channels` or `/api/channels/{id}/messages`**. A live `GET /api/channels` returns 404. AC-6 (auth required) is the one cleanly-covered AC because the handler-level test exercises the real `auth.RequireJWT` middleware. Same wiring-gap pattern as `feature-file-perms-and-headers` (PR #37).

**Phase-1 sibling test-agent PRs in flight (not yet on main):** PR #37 (`file-perms-and-headers`), PR #43 (`body-and-ws-caps`), PR #47 (`auth-internals` + `security-headers-and-sqlite-ensure-wiring` stub), PR #48 (`startup-config-checks`), PR #50 (`auth-endpoints`), PR #52 (`access-log-fields-and-wiring` stub), PR #53 (`rate-limits`). Findings docs appear in the index once each PR merges.

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
