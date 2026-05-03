# Test plan: Monorepo scaffold

**Feature plan:** [feature-monorepo-scaffold.md](../feature-monorepo-scaffold.md)
**Parent phase:** [Phase 0: Walking skeleton — system-test ready](../../phase-0-walking-skeleton-system-test-ready.md)
**PRD revision:** 7e33be3

## Note on requirement IDs

The feature plan carries no `requirement_ids` — this scaffold is verified against the acceptance criteria directly. Per the test plan rules, every requirement needs both unit and E2E coverage; here, the testable units are the acceptance criteria (AC-1..AC-5), and tests carry those IDs for grep-ability.

The scaffold itself is configuration (Go workspace file, pnpm workspace file, root `package.json`, `.gitignore`). There is no application code to unit-test in the conventional sense, so "unit tests" below are file-shape assertions that can run without spinning up the workspaces, and "E2E tests" are end-to-end shell invocations from a clean clone.

## Coverage matrix

| Requirement ID | Description | Unit tests | E2E tests |
|----------------|-------------|------------|-----------|
| AC-1 | `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages | 1 | 1 |
| AC-2 | `pnpm-workspace.yaml` declares `apps/*` and `packages/*` | 1 | 1 |
| AC-3 | Root `package.json` exposes `dev`, `build`, `test` scripts that fan out across workspaces | 1 | 1 |
| AC-4 | `pnpm install` from a clean clone succeeds without errors | 0 | 1 |
| AC-5 | Each top-level script (`dev`, `build`, `test`) completes without configuration errors | 0 | 3 |

## Unit tests

Unit tests here are static-shape checks on the scaffolding files. They parse the file and assert structure without executing the build/test/dev pipeline.

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
  - **Target file:** `tests/scaffold/pnpm_workspace_test.ts` (or equivalent script)
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

## E2E tests

E2E tests run from a clean clone (or a clean working tree with `node_modules` and Go build caches removed) and exercise the actual tooling.

### AC-1 — `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages

- **Name:** `TestAC1_GoWorkSyncSucceeds_E2E`
  - **Target file:** `tests/e2e/scaffold/go_work_e2e_test.go`
  - **Scenario:** from a clean clone, run `go work sync` at the repo root
  - **Asserts:**
    - command exits 0
    - stderr contains no "no such module" or "directory not found" errors

### AC-2 — `pnpm-workspace.yaml` declares `apps/*` and `packages/*`

- **Name:** `it('AC-2: pnpm sees declared workspaces from a clean clone', ...)`
  - **Target file:** `tests/e2e/scaffold/pnpm-workspaces.spec.ts`
  - **Scenario:** from a clean clone, run `pnpm -r exec pwd` (or `pnpm m ls --json`)
  - **Asserts:**
    - command exits 0
    - any workspace package created in this phase under `apps/*` or `packages/*` appears in the listing

### AC-3 — Root `package.json` scripts fan out across workspaces

- **Name:** `it('AC-3: root scripts dispatch to workspaces via pnpm -r', ...)`
  - **Target file:** `tests/e2e/scaffold/root-scripts.spec.ts`
  - **Scenario:** from a clean clone, run `pnpm run build --dry-run` (or equivalent) and inspect what pnpm reports it would run
  - **Asserts:**
    - exit code 0
    - output references the recursive/workspace dispatch (no "Missing script" error at root level)

### AC-4 — `pnpm install` from a clean clone succeeds

- **Name:** `it('AC-4: pnpm install on a clean clone exits 0', ...)`
  - **Target file:** `tests/e2e/scaffold/pnpm-install.spec.ts`
  - **Scenario:** clone (or `git clean -fdx` + `rm -rf node_modules`) → run `pnpm install`
  - **Asserts:**
    - exit code 0
    - no `ERR_PNPM_*` errors on stderr
    - `node_modules` and `pnpm-lock.yaml` are produced

### AC-5 — Each top-level script completes without configuration errors

- **Name:** `it('AC-5: pnpm run dev exits without "Missing script" or workspace-config errors', ...)`
  - **Target file:** `tests/e2e/scaffold/pnpm-run-dev.spec.ts`
  - **Scenario:** after `pnpm install`, run `pnpm run dev` and terminate it after a short timeout if it would otherwise stay foregrounded (dev servers may run indefinitely; the assertion is that it starts cleanly, not that it stays up)
  - **Asserts:**
    - exit code is 0 OR the process is alive past the startup window with no config errors emitted
    - stderr contains no "Missing script", "ENOENT", or "no projects matched" messages

- **Name:** `it('AC-5: pnpm run build exits 0 with stub workspaces', ...)`
  - **Target file:** `tests/e2e/scaffold/pnpm-run-build.spec.ts`
  - **Scenario:** after `pnpm install`, run `pnpm run build`
  - **Asserts:**
    - exit code 0
    - stderr contains no "Missing script" or workspace-config errors
    - acceptable for individual workspaces to be no-ops at this phase

- **Name:** `it('AC-5: pnpm run test exits 0 with stub workspaces', ...)`
  - **Target file:** `tests/e2e/scaffold/pnpm-run-test.spec.ts`
  - **Scenario:** after `pnpm install`, run `pnpm run test`
  - **Asserts:**
    - exit code 0
    - stderr contains no "Missing script" or workspace-config errors

## Coverage rules
- Every acceptance criterion (AC-1..AC-5) has at least one E2E test; AC-1..AC-3 also have a unit-style static check.
- AC-4 and AC-5 are inherently end-to-end (they describe runtime behaviour of the package manager) and have no meaningful unit-level analogue — flagged here so the gap is explicit.
- Test names start with the AC ID for grep-ability.
- Tests describe behaviour from the acceptance criteria, not implementation details of the YAML/JSON files beyond the structural shape the criteria require.
