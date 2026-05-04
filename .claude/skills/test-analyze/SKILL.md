---
name: test-analyze
description: Independent E2E coverage analysis. Scan feature specs under `specs/plans/phase-*/feature-*.md` and the agent-owned E2E tests under `tests/e2e/`; for each acceptance criterion, decide whether an E2E test in the agent's tree references it. Write per-feature findings markdown to `specs/test-analysis/`. Invoked by `/test-watch`, also runnable standalone for ad-hoc analysis.
user-invocable: true
allowed-tools: [Read, Write, Edit, Bash, Glob, Grep]
---

# /test-analyze — independent E2E coverage analysis

You are an independent tester. You scan the repo's feature specs, extract acceptance criteria (ACs), and check whether each AC has a corresponding **end-to-end test that the test-analysis agent owns**. You write structured findings to markdown.

The agent's tests live exclusively under `tests/e2e/<phase>/<feature-slug>/`. Tests written by feature authors (in-package `*_test.go` under `apps/**` / `packages/**`, or the pre-existing top-level test suites at `tests/scaffold/`, `tests/smoke-test/`, `tests/server-ws-hub/`, `tests/monorepo-scaffold/`) are **not** coverage for this analysis. They serve a different purpose — unit/integration coverage of the implementation — and the test-analysis agent provides an independent black-box layer on top.

## Arguments

`$ARGUMENTS` — optional path to operate in (a worktree). Defaults to the current working directory. Treat this path as `$ROOT` for the rest of this skill.

## Inputs

- Feature specs: `$ROOT/specs/plans/phase-*/feature-*.md`. Each has an `## Acceptance criteria` section with bulleted criteria.
- Agent-owned E2E tests: `$ROOT/tests/e2e/<phase>/<feature-slug>/**/*_test.go` and `$ROOT/tests/e2e/<phase>/<feature-slug>/**/*.{test,spec}.{ts,tsx,mts,cts,js,jsx,mjs,cjs}`.
- Source code: `$ROOT/apps/**`, `$ROOT/packages/**`. Read to understand what's actually implemented (so the agent can write meaningful E2E tests against the real surface).

Do NOT scan or count tests under: `$ROOT/apps/**/*_test.go`, `$ROOT/packages/**/*_test.*`, `$ROOT/tests/scaffold/`, `$ROOT/tests/smoke-test/`, `$ROOT/tests/server-ws-hub/`, `$ROOT/tests/monorepo-scaffold/`, `node_modules/`, `.claude/worktrees/`, `dist/`, `bin/`. Only `tests/e2e/` counts.

## AC ID convention

For each feature spec:
1. Locate the `## Acceptance criteria` section.
2. Number each top-level bullet `AC-1`, `AC-2`, … in order. The spec itself may not write the IDs — that's fine; you assign them positionally and they become the canonical reference.
3. If a bullet is a sub-bullet of another, treat it as part of the parent AC, not a separate AC.
4. Once an AC has been assigned an ID in a previous run (visible in the existing findings file or in `tests/e2e/<phase>/<feature-slug>/` test names), **do not renumber** it. Stable IDs are load-bearing — tests reference them by name.

## Coverage detection (strict)

For each `AC-N` of a feature:
- Search **only** under `$ROOT/tests/e2e/<phase>/<feature-slug>/` for the literal token `AC-N` in:
  - Go test function names (`TestACN_…`)
  - Go file-leading comments
  - vitest `describe(…)` / `it(…)` strings
- A literal `AC-N` match in any of those locations = **covered**.
- A test that contains the `AC-N` token but is marked `t.Skip(...)` / `it.skip(...)` and the implementation is live = **partial** (the AC is on record but not currently exercised; flag it).
- A test that contains the `AC-N` token but the implementation is `stub` = **deferred** (correct state — the test is skipped because impl doesn't exist yet).
- No test under the per-feature directory contains the `AC-N` token = **missing**.

Do not infer coverage from paraphrasing, from tests in other directories, or from in-package tests that the feature author wrote. The signal must be: the agent's own test, in the agent's own directory, naming the AC explicitly.

## Implementation status detection

For each feature, infer whether the production code exists yet:
- For Go server features: check `$ROOT/apps/server/` and the relevant `internal/` packages. If the spec's "Files expected to be touched" list is mostly absent, status is `stub`.
- For CLI features: check `$ROOT/apps/cli/`. If only `main.go` with a stub exists, `stub`.
- For client packages: check `$ROOT/packages/<name>/`. If only a `doc.go` or empty package, `stub`.
- For workspace/scaffold features: check the relevant config files (`go.mod`, `pnpm-workspace.yaml`, `package.json`).
- Status values: `implemented`, `partial`, `stub`, `unknown`.

ACs whose implementation is `stub` get marked **deferred** in findings — when the test gets written, it will be a skipped test. As soon as the impl lands, the corresponding test should be unskipped.

## Reading existing E2E tests

Before declaring an AC missing, **read** the existing E2E test files under `tests/e2e/<phase>/<feature-slug>/` (if any). The goal is twofold:
1. Avoid declaring an AC missing when an existing test actually exercises it but the AC token is only present nearby — flag this as a borderline case in the findings so `/test-implement` either renames the test or adds an anchor comment.
2. Identify shared helpers (server-startup, ticket-mint, ws-dial) the next test should reuse instead of duplicating. Surface those helper paths in the findings so `/test-implement` doesn't write a third copy.

## Output: per-feature findings doc

For each feature spec at `specs/plans/<phase>/feature-<slug>.md`, write:

`$ROOT/specs/test-analysis/<phase>/<slug>.md`

Use this exact template:

```markdown
---
feature: <slug>
phase: <phase>
analyzed_at: <ISO-8601 timestamp>
analyzed_commit: <full SHA of HEAD>
implementation_status: <implemented|partial|stub|unknown>
total_acs: <int>
covered: <int>
partial: <int>
missing: <int>
deferred: <int>
---

# E2E test analysis: <feature title>

**Spec:** `specs/plans/<phase>/feature-<slug>.md`
**Implementation status:** <status> — <one-line evidence, e.g. "apps/server/internal/auth contains handlers + tests at SHA xyz">
**E2E test directory:** `tests/e2e/<phase>/<feature-slug>/` (<N existing test files | does not exist yet>)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | <verbatim AC text> | covered | `tests/e2e/<phase>/<slug>/auth_test.go::TestAC1_...` |
| AC-2 | <verbatim AC text> | missing | — |
| AC-3 | <verbatim AC text> | deferred | impl is stub; `tests/e2e/<phase>/<slug>/auth_test.go::TestAC3_...` (skipped) |

## Findings

### Missing E2E tests

For each missing AC, one short paragraph that gives `/test-implement` enough to write the test without re-deriving it:

- **AC-N — <one-line summary of the AC>.**
  - **What to assert:** the observable behavior the test must prove.
  - **Layer:** Go (boot server binary) | vitest (TS workspace) | bash (system).
  - **File path:** `tests/e2e/<phase>/<slug>/<short>_test.go` (or `.test.ts`).
  - **Setup it needs:** real binaries built in tempdir, random JWT secret + invite code via `crypto/rand`, sqlite in `t.TempDir()`, etc.
  - **Helpers it can reuse:** point at any existing helper in the per-feature dir (or in a sibling dir) that already does server-startup, login, ticket-mint, etc. If none exists yet, say "no helper yet — first test will need to define it."

### Deferred E2E tests

For each deferred AC: same structure, plus the impl gap that must close before the test can run live ("apps/web does not exist yet — un-skip when the directory has a `package.json`").

### Partial / suspect coverage

For each partial: which test contains the AC token, why it's partial (e.g. asserts only the happy path, or is `t.Skip`'d while the impl is live), and what would close the gap.

### Helpers and harness notes

Optional. If the feature's E2E directory already exists, note the helpers it exposes (`startServer(t)`, `mintTicket(t, srv, userID)`, `dialWS(t, srv, ticket)`) so the next test author reuses them. If the harness should grow a new helper, suggest the signature.

## Recommendations for /test-implement

A short bulleted list. Each bullet is one concrete task — a file to create, a test to add, a helper to extract. Order by dependency (helpers first, then the tests that use them).
```

## Output: index README

Write `$ROOT/specs/test-analysis/README.md`:

```markdown
# E2E test analysis

Generated by the test-analysis agent (see `.claude/skills/test-watch/SKILL.md`). The agent is an independent tester: it reads each feature spec's acceptance criteria and writes E2E tests under `tests/e2e/<phase>/<feature-slug>/` that drive the production system end-to-end.

In-package tests under `apps/**` / `packages/**` belong to feature authors and are NOT counted in this analysis. The numbers below reflect only the agent's E2E coverage.

**Last updated:** <ISO-8601>
**Analyzed commit:** <SHA>

| Phase | Feature | Impl status | E2E covered | Partial | Missing | Deferred |
|-------|---------|-------------|-------------|---------|---------|----------|
| phase-0 | monorepo-scaffold | implemented | 3/3 | 0 | 0 | 0 |
| phase-0 | server-ws-hub | implemented | 0/4 | 0 | 4 | 0 |
| ... | ... | ... | ... | ... | ... | ... |

**Totals:** <X> features, <Y> ACs, <C> covered, <P> partial, <M> missing, <D> deferred.
```

## What to return to the caller

When invoked by `/test-watch`, after writing files, emit a single chat line in this exact format so the orchestrator can parse it:

```
test-analyze: features=<F> total_acs=<T> covered=<C> partial=<P> missing=<M> deferred=<D>
```

Plus, immediately after, one line per feature with at least one missing or deferred AC, ordered by missing-count descending:

```
feature: <phase>/<slug> missing=<M> deferred=<D> findings=specs/test-analysis/<phase>/<slug>.md
```

The orchestrator uses these lines to pick which single feature `/test-implement` should work on this tick.

## Things you must NOT do

- Do not modify any file outside `$ROOT/specs/test-analysis/` AND `$ROOT/.git/` AND the GitHub side via `gh` (issue creates / sub-issue links). Code stays untouched here.
- Do not invent ACs that aren't in the spec, and do not renumber existing AC IDs that tests already reference.
- Do not skip features whose spec has no `## Acceptance criteria` section — write a findings doc that says so explicitly (`total_acs: 0`, recommendation: "spec lacks AC section, please add").
- Do not write findings for files outside `specs/plans/phase-*/feature-*.md` (e.g. don't analyze the phase overview docs).
- Do not count in-package tests, scaffold tests, smoke tests, or pre-existing top-level suites as coverage. Only `tests/e2e/<phase>/<feature-slug>/` counts.
- Do not paraphrase-match. Coverage requires the literal `AC-N` token in a test name or describe string within the per-feature E2E directory.
