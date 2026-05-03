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
**Last updated:** 2026-05-03T15:46:48Z
**Analyzed commit:** `5b7953c`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [logging-and-error-envelope](phase-1/logging-and-error-envelope.md) | partial | 3/4 | 1 | 0 | 0 |
| phase-1 | [auth-endpoints](phase-1/auth-endpoints.md) | implemented | 7/7 | 0 | 0 | 0 |

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred.

**Phase-1 totals (so far):** 2 features analyzed of 11 spec'd · 11 ACs · 10 covered · 1 partial · 0 missing · 0 deferred. AC-1 of `logging-and-error-envelope` remains partial (access-log line missing IP).

`feature-auth-endpoints` (PR #38) ships clean with 25+ in-package tests across the 5 endpoints + ticket store + middleware + auth-events recording. `scripts/smoke.sh` was updated to drive register → login → ws-ticket → watch and exits 0 against the live binary.

**Cross-feature observation worth surfacing:** `apps/server/main.go` now wires `cfg.JWTSecret` (validated by `feature-startup-config-checks`) into `httpapi.NewAuthHandlers.SigningKey`. This closes the `feature-auth-internals` AC-5 partial flag from PR #47 — the next test-watch tick should re-promote it.

**Phase-1 sibling test-agent PRs in flight (not yet on main):** PR #37 (file-perms-and-headers — likely superseded by wiring spec), PR #40 (sqlite-schema-and-ulid 4/5), PR #43 (body-and-ws-caps 4/4), PR #47 (auth-internals 4/5 + security-headers wiring stub), PR #48 (startup-config-checks 5/5). Findings docs land in the index as their PRs merge.

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
