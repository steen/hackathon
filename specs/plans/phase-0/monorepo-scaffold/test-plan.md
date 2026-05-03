# Test plan: Monorepo scaffold

**Feature plan:** [feature-monorepo-scaffold.md](../feature-monorepo-scaffold.md)
**Parent phase:** [Phase 0: Walking skeleton — system-test ready](../../phase-0-walking-skeleton-system-test-ready.md)
**PRD revision:** 7e33be3

## Note on requirement IDs

The feature plan carries no `requirement_ids` — this scaffold is verified against the acceptance criteria directly. Per the test plan rules, every requirement needs both unit and E2E coverage; here, the testable units are the acceptance criteria (AC-1..AC-5), and tests carry those IDs for grep-ability.

The scaffold itself is configuration (Go workspace file, pnpm workspace file, root `package.json`, `.gitignore`). There is no application code to unit-test in the conventional sense, so "unit tests" below are file-shape assertions that can run without spinning up the workspaces. End-to-end verification — that the scaffold actually produces a working install/build/test loop — is performed by the CI workflow itself, which runs the exact commands the acceptance criteria describe on every PR and main-branch push.

## Coverage matrix

| Requirement ID | Description | Unit tests | E2E coverage |
|----------------|-------------|------------|--------------|
| AC-1 | `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages | 1 | CI: `go build` + `go test` per workspace module |
| AC-2 | `pnpm-workspace.yaml` declares `apps/*` and `packages/*` | 1 | CI: `pnpm install --frozen-lockfile` (resolves the workspace globs) |
| AC-3 | Root `package.json` exposes `dev`, `build`, `test` scripts that fan out across workspaces | 1 | CI: `pnpm -r --if-present build` + `pnpm -r --if-present test` |
| AC-4 | `pnpm install` from a clean clone succeeds without errors | 0 | CI: `pnpm install --frozen-lockfile` runs on a fresh `actions/checkout` |
| AC-5 | Each top-level script (`dev`, `build`, `test`) completes without configuration errors | 0 | CI: `pnpm -r --if-present build` and `pnpm -r --if-present test` (dev is interactive — not exercised in CI by design) |

## Unit tests

Unit tests are static-shape checks on the scaffolding files. They parse the file and assert structure without executing the build/test/dev pipeline.

### AC-1 — `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages

- **Name:** `TestAC1_GoWorkDeclaresExpectedModules`
  - **Target file:** `tests/scaffold/go_work_test.go`
  - **Asserts:**
    - `go.work` exists at repo root
    - parsed `use (...)` block contains entries for `./apps/server` and `./apps/cli`
    - any shared Go package directory created in this phase appears in the `use` block
    - `go` directive specifies a version (no empty/missing toolchain line)

### AC-2 — `pnpm-workspace.yaml` declares `apps/*` and `packages/*`

- **Name:** `TestAC2_PnpmWorkspaceDeclaresGlobs`
  - **Target file:** `tests/scaffold/pnpm_workspace_test.ts`
  - **Asserts:**
    - `pnpm-workspace.yaml` exists at repo root
    - YAML parses cleanly
    - `packages` field contains `apps/*` and `packages/*`

### AC-3 — Root `package.json` exposes `dev`, `build`, `test` scripts

- **Name:** `it('AC-3: root package.json declares dev/build/test scripts that fan out', ...)`
  - **Target file:** `tests/scaffold/root-package-json.test.ts`
  - **Asserts:**
    - `package.json` exists at repo root
    - JSON parses
    - `private` is `true`
    - `scripts.dev`, `scripts.build`, `scripts.test` are all present and non-empty strings
    - each of those scripts invokes a workspace-fanning command (e.g. contains `pnpm -r` or equivalent recursive invocation)

## E2E coverage via CI

End-to-end verification is performed by `.github/workflows/ci.yml` rather than by a dedicated test directory under `tests/e2e/`. The CI workflow runs the same commands the acceptance criteria describe, on a clean GitHub-hosted runner, on every PR and main-branch push. A red CI run blocks the merge.

| AC | CI step that exercises it |
|----|---------------------------|
| AC-1 | `Build (each module)` and `Test (each module)` in the `go` job iterate every entry from `go.work` |
| AC-2 | `pnpm install --frozen-lockfile` resolves the workspace globs declared in `pnpm-workspace.yaml`; failure indicates a malformed glob |
| AC-3 | `pnpm -r --if-present build` and `pnpm -r --if-present test` exercise the recursive fan-out from the root scripts |
| AC-4 | `pnpm install --frozen-lockfile` runs on a fresh `actions/checkout` (equivalent to a clean clone) |
| AC-5 | The same `pnpm -r --if-present build` / `test` steps verify those scripts complete cleanly. `dev` is intentionally not exercised in CI — it is a long-running interactive command |

## Coverage rules
- AC-1..AC-3 have a unit-style static check; AC-1..AC-5 are exercised end-to-end by the CI workflow.
- AC-4 and AC-5 are inherently end-to-end (they describe runtime behaviour of the package manager) and have no meaningful unit-level analogue — verified by CI execution.
- Test names start with the AC ID for grep-ability.
- Tests describe behaviour from the acceptance criteria, not implementation details of the YAML/JSON files beyond the structural shape the criteria require.

## History
- An earlier revision of this plan placed AC-1..AC-5 e2e tests under `tests/e2e/scaffold/`. Those tests had structural bugs (notably an `existsSync(node_modules)` assertion that failed because the tmpdir had no dependencies) and were removed in commit `ddc05ab`. End-to-end verification is now performed by the CI workflow itself, which runs the same commands on every PR.
