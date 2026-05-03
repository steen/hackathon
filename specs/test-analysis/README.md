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
**Last updated:** 2026-05-03T16:13:00Z
**Analyzed commit:** `f320ff3`

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
| phase-1 | [ws-hardening](phase-1/ws-hardening.md) | partial | 2/4 | 1 | 0 | 1 |

**Phase-0 totals:** 4 features · 20 ACs · 20 covered · 0 partial · 0 missing · 0 deferred.

**Phase-1 totals (so far):** 7 features analyzed of 13 spec'd · 31 ACs · 22 covered · 4 partial · 0 missing · 5 deferred.

Notable phase-1 gaps:
- `auth-internals` AC-5 partial: signing-key-from-config wiring (now wired by `feature-startup-config-checks` + `feature-auth-endpoints` PR #50). Next tick reanalyzes and promotes.
- `logging-and-error-envelope` AC-1 partial: access-log line missing `IP` field — closes when `feature-access-log-fields-and-wiring` (PR #52 stub) implementation ships.
- `sqlite-schema-and-ulid` AC-4 partial: schema permits ULIDs but no shipped INSERT path used `ids.NewULID()` at the analyzed SHA. Closed when `feature-channels-and-messages` (PR #55) findings merge.
- `security-headers-and-sqlite-ensure-wiring`: planned-only stub spec; impl not started.
- `ws-hardening` AC-3 partial + AC-4 deferred: handler extracts userID from ticket but stashes it in `_ = userID` with a TODO; AC-4 typed-channel-not-found frame waits on a typed inbound WS frame contract that `feature-channels-and-messages` was meant to introduce but didn't (still raw byte rebroadcast).

**Phase-1 sibling test-agent PRs in flight (not yet on main):** PR #37 (`file-perms-and-headers`), PR #43 (`body-and-ws-caps`), PR #50 (`auth-endpoints`), PR #52 (`access-log-fields-and-wiring` stub), PR #53 (`rate-limits`), PR #55 (`channels-and-messages`), PR #56 (`auth-endpoint-paths-align-with-prd` stub).

**Phases 2–3:** specs exist (`specs/plans/phase-{2,3}/feature-*.md`) but have not been analyzed yet. The agent will pick them up once their implementation commits land on `main`.
<!-- AGENT-INDEX-END -->
