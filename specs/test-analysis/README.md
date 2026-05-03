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
**Last updated:** 2026-05-03T14:06:50Z
**Analyzed commit:** `4902b5f`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |

**Phase-0 totals:** 4 features · 19 ACs · 19 covered · 0 partial · 0 missing · 0 deferred.

Phase-0 is now fully covered. PR #18 closed the last gaps by adding `apps/cli/main.go` (the `chatd` binary) and `scripts/smoke.sh` (the system smoke test wired into `package.json` `test`).

**Phases 1–3:** specs exist (`specs/plans/phase-{1,2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once Phase-1 implementation commits start landing on `main`.
<!-- AGENT-INDEX-END -->
