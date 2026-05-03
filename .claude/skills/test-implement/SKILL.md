---
name: test-implement
description: Read findings docs from `specs/test-analysis/` and write the missing tests under `tests/`. Follows the existing `tests/scaffold/` patterns (Go for Go ACs, vitest for TS/workspace ACs). Skipped tests for `deferred` ACs whose impl doesn't exist yet. Invoked by `/test-watch`, also runnable standalone.
user-invocable: true
allowed-tools: [Read, Write, Edit, Bash, Glob, Grep]
---

# /test-implement — write the missing tests

You read the findings produced by `/test-analyze` and translate them into actual test files. You do NOT modify production code under `apps/**` or `packages/**`.

## Arguments

`$ARGUMENTS` — optional path to operate in (a worktree). Defaults to the current working directory. Treat this path as `$ROOT`.

## Inputs

- All `$ROOT/specs/test-analysis/*/*.md` files written by `/test-analyze`.
- Existing test patterns at `$ROOT/tests/scaffold/`:
  - `go_module_test.go` — Go style: package `<name>_test`, `TestACN_*` function names, AC ID anchored in a leading comment, table-style assertions with `t.Errorf("AC-N: …")`.
  - `pnpm_workspace_test.ts`, `root-package-json.test.ts` — vitest style: `describe("AC-N: …", …)`, `it("AC-N: …", …)`, `expect()` chain.
- `$ROOT/tests/vitest.config.ts` — vitest config (test file glob, timeouts).
- `$ROOT/tests/package.json` — vitest dep version.

## Decision: which test layer?

For each missing AC in findings:

| AC concerns | Layer | Where the test goes |
|-------------|-------|---------------------|
| Go module/source code (`go.mod`, `apps/cli`, `apps/server`) | Go | `$ROOT/tests/<area>/<feature>_test.go` (package `<area>_test`) |
| Workspace config / package.json / pnpm | vitest | `$ROOT/tests/<area>/<feature>.test.ts` |
| End-to-end CLI/server interaction (system test) | bash + Go binary | `$ROOT/scripts/smoke.sh` (only create/extend if findings explicitly call for it) |

If an AC could be either: prefer the layer the existing scaffold for that feature uses; if no scaffold exists, prefer Go for code-level facts and vitest for config-level facts.

## File and identifier conventions

- Per-feature test directory: `$ROOT/tests/<feature-slug>/`. If it doesn't exist, create it. Don't dump everything into `tests/scaffold/` — that's reserved for the original scaffold ACs.
- Go test file: `<feature>_test.go`. Package: `<feature>_test`. Function name: `TestACN_<CamelCaseAcSummary>`.
- TS test file: `<feature>.test.ts`. Use `describe("AC-N: <verbatim AC>", () => { it("AC-N: <same>", () => { ... }) })`.
- Every test must include the literal token `AC-N` and the feature slug somewhere visible (function name or describe string) so `/test-analyze` will detect it as covered on the next run.

## Deferred ACs

For ACs whose findings status is `deferred` (impl is `stub`):
- Still write the test file, but mark the test skipped:
  - Go: `t.Skip("AC-N: deferred — apps/<x> is stub; see specs/plans/<phase>/feature-<slug>.md")`
  - vitest: `it.skip("AC-N: …", () => { /* deferred — ... */ })`
- The skipped test is still a positive signal: the AC is on record, will be picked up automatically once impl lands and someone removes the skip.

## Partial ACs

Findings flagged `partial` mean an existing test mentions the AC ID but might not fully exercise it. Read the existing test, then either:
- Tighten the existing assertions in place (preferred — minimal new code).
- Add a sibling test covering the gap, with a comment pointing to the partial finding.

Don't duplicate a working test.

## Helper code rules

- Reuse helpers (`repoRoot(t)` etc.) from `tests/scaffold/go_module_test.go` if relevant. If a test outside `tests/scaffold/` needs the same helper, copy it locally rather than introducing a shared internal package — no new abstractions until there are 3+ call sites.
- For vitest, `import { describe, it, expect } from "vitest"` and use `node:fs`/`node:path` from the standard library. Avoid adding dependencies; if a finding genuinely needs a new dep, surface it in the chat output and skip the test rather than adding it silently.

## After writing

Run the relevant test commands inside `$ROOT` to verify:

```
cd "$ROOT" && go test ./...
cd "$ROOT" && pnpm install --frozen-lockfile && pnpm -r --if-present test
```

(If `pnpm` is not on PATH, use `~/.npm-global/bin/pnpm`.)

For each newly written test, the expected outcome is:
- **Skipped** if AC is deferred (test runs but is marked skipped).
- **Passing** if AC is live and the production code already satisfies the AC.
- **Failing** if AC is live and the production code does NOT satisfy it. This is a real signal — leave the test failing, do not modify production code to make it pass. Surface the failure in chat and append to the findings doc.

## What to return to the caller

Emit one chat line in this exact format so `/test-watch` can parse:

```
test-implement: written=<N> skipped=<S> passing=<P> failing=<F>
```

Plus, for each failing test, one extra line:
```
failing: <test path>::<test name> — <one-line reason from output>
```

## Things you must NOT do

- Do not edit anything under `apps/**` or `packages/**`. Tests only.
- Do not delete or rewrite tests that `/test-analyze` marked as `covered`.
- Do not add new npm or Go dependencies without surfacing them in chat first.
- Do not touch `scripts/smoke.sh` unless a finding explicitly says to (the system test is its own thing, owned by phase 0 plan).
- Do not narrate every file you write in chat; the summary line at the end is the report.
