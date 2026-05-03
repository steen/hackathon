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
**Last updated:** 2026-05-03T15:24:52Z
**Analyzed commit:** `440d18f`

| Phase | Feature | Status | Covered | Partial | Missing | Deferred |
|-------|---------|--------|---------|---------|---------|----------|
| phase-0 | [monorepo-scaffold](phase-0/monorepo-scaffold.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [server-ws-hub](phase-0/server-ws-hub.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-0 | [cli-send-watch](phase-0/cli-send-watch.md) | implemented | 4/4 | 0 | 0 | 0 |
| phase-0 | [smoke-test](phase-0/smoke-test.md) | implemented | 5/5 | 0 | 0 | 0 |
| phase-1 | [logging-and-error-envelope](phase-1/logging-and-error-envelope.md) | partial | 3/4 | 1 | 0 | 0 |
| phase-1 | [auth-internals](phase-1/auth-internals.md) | implemented | 4/5 | 1 | 0 | 0 |
| phase-1 | [security-headers-and-sqlite-ensure-wiring](phase-1/security-headers-and-sqlite-ensure-wiring.md) | stub | 0/4 | 0 | 0 | 4 |

**Phase-0 totals:** 4 features · 19 ACs · 19 covered · 0 partial · 0 missing · 0 deferred.
**Phase-1 totals (so far):** 3 features analyzed of 11 spec'd · 13 ACs · 7 covered · 2 partial · 0 missing · 4 deferred.

Notable phase-1 gaps:
- `auth-internals` AC-5 partial: signing-key-from-config wiring belongs to `feature-startup-config-checks` (not yet implemented).
- `logging-and-error-envelope` AC-1 partial: access-log line missing `IP` field (production-code fix; PR #32 findings flag it).
- `security-headers-and-sqlite-ensure-wiring`: spec landed as `planned` to track gaps the agent flagged earlier; impl not started.

**Phase-1 sibling test-agent PRs in flight (not yet on main):** PR #37 tracks `file-perms-and-headers` (1/3, 2 partial — superseded by the new wiring spec); PR #40 tracks `sqlite-schema-and-ulid` (4/5, AC-4 partial); PR #43 tracks `body-and-ws-caps` (4/4 covered). Findings docs land in the index as their PRs merge.

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet.
<!-- AGENT-INDEX-END -->
