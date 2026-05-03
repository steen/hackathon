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
**Last updated:** 2026-05-03T15:15:00+02:00
**Analyzed commit:** `206b9e2`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 3/5 | 0 | 2 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | stub | 0/5 | 0 | 0 | 5 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | stub | 0/4 | 0 | 0 | 4 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | stub | 0/5 | 0 | 0 | 5 |

**Phase-0 totals:** 4 features · 19 ACs · 3 covered · 2 missing · 14 deferred.

**Phases 1–3:** specs exist (`specs/plans/phase-{1,2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up automatically once phase-0 advances and new commits land on `main`.
<!-- AGENT-INDEX-END -->
