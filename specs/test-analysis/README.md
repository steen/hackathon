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
**Last updated:** 2026-05-03T15:02:47Z
**Analyzed commit:** `e689e8f`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [body-and-ws-caps](phase-1/body-and-ws-caps.md) | implemented | 4/4 | 0 | 0 | 0 |

**Phase-0 totals:** 4 features · 19 ACs · 19 covered · 0 partial · 0 missing · 0 deferred. (PRs #25, #32, #35 are open and rework totals once merged; this index reflects current `main`.)
**Phase-1 totals (so far):** 1 feature analyzed of 10 spec'd · 4 ACs · 4 covered · 0 partial · 0 missing · 0 deferred.

`feature-body-and-ws-caps` (PR #27) ships clean: WS read limit (64 KiB → close 1009), body cap (4 KiB), per-conn token bucket (close 1008), and REST 16 KiB cap (413). All four ACs covered by package-level tests with explicit library-constant guards.

**Phase-1 sibling PRs in flight (not yet on main):** PR #32 tracks `logging-and-error-envelope` (3/4, AC-1 IP gap); PR #37 tracks `file-perms-and-headers` (1/3, SecurityHeaders not wired); PR #40 tracks `sqlite-schema-and-ulid` (4/5, AC-4 partial pending insert code). Findings docs land in the index as their PRs merge.

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet.
<!-- AGENT-INDEX-END -->
