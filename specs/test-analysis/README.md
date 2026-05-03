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
**Last updated:** 2026-05-03T14:56:48Z
**Analyzed commit:** `f765726`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 6/6 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [logging-and-error-envelope](phase-1/logging-and-error-envelope.md) | partial | 3/4 | 1 | 0 | 0 |
| phase-1 | [sqlite-schema-and-ulid](phase-1/sqlite-schema-and-ulid.md) | implemented | 4/5 | 1 | 0 | 0 |

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred. PR #25 added a 6th AC to `server-ws-hub` (the new `/debug/subs` endpoint), fully covered by `apps/server/internal/wsapi/debug_handler_test.go`.

**Phase-1 totals (so far):** 2 features analyzed of 10 spec'd · 9 ACs · 7 covered · 2 partial · 0 missing · 0 deferred. AC-1 of `logging-and-error-envelope` is partial because the access-log line omits `IP` (production gap, not a test gap). AC-4 of `sqlite-schema-and-ulid` is partial because the schema permits ULIDs (TEXT PRIMARY KEY) and `ids.NewULID()` exists with strong tests, but no shipped INSERT code path uses it yet — `repo.Repo` is a constructor-only stub; firms up once `feature-channels-and-messages` lands.

**Phase-1 sibling PRs in flight (not yet on main):** PR #37 tracks `file-perms-and-headers` (1/3, SecurityHeaders not wired). Findings docs appear in the index once each PR merges; the next tick after each merge reconciles totals.

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
