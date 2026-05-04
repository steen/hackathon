---
feature: monorepo-scaffold
phase: phase-0
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: Monorepo scaffold

**Spec:** `specs/plans/phase-0/feature-monorepo-scaffold.md`
**Implementation status:** implemented — root `go.mod` declares `module hackathon`, `pnpm-workspace.yaml` lists `apps/*`, `packages/*`, and `tests`, and `package.json` exposes `dev`, `build`, `smoke`, `test`, `lint` scripts.
**E2E test directory:** `tests/e2e/phase-0/monorepo-scaffold/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | A single root `go.mod` declares module name `hackathon`; all Go code under `apps/` and `packages/` lives in this one module and imports use the form `hackathon/<path>`. | missing | — |
| AC-2 | `pnpm-workspace.yaml` declares `apps/*` and `packages/*` workspaces. | missing | — |
| AC-3 | Root `package.json` exposes `dev`, `build`, and `test` scripts that fan out to the relevant apps/packages. | missing | — |
| AC-4 | Running `pnpm install` from a clean clone succeeds without errors. | missing | — |
| AC-5 | Running each top-level script (`dev`, `build`, `test`) completes without configuration errors (script bodies may be stubs at this stage). | missing | — |

## Findings

### Missing E2E tests

- **AC-1 — Single root `go.mod` with module `hackathon`; all imports use `hackathon/<path>`.**
  - **What to assert:** Exactly one `go.mod` file exists at the repo root (no `apps/*/go.mod`, no `packages/*/go.mod`); its `module` directive is `hackathon`. Every Go file under `apps/` and `packages/` whose imports reference an internal repo path uses the `hackathon/...` prefix and never `github.com/...` for internal code.
  - **Layer:** Go (no server boot needed; this is a static repo-shape check).
  - **File path:** `tests/e2e/phase-0/monorepo-scaffold/gomod_test.go`
  - **Setup it needs:** `repoRoot(t)` helper modeled on `tests/server-ws-hub/hub_test.go::repoRoot`; `filepath.WalkDir`; parse `go.mod` with a `bufio.Scanner` (one-line `module` directive — no need to add a parser dep).
  - **Helpers it can reuse:** `repoRoot(t)` from the gold-standard harness.

- **AC-2 — `pnpm-workspace.yaml` declares `apps/*` and `packages/*`.**
  - **What to assert:** `<root>/pnpm-workspace.yaml` exists; its content includes both `apps/*` and `packages/*` entries under `packages:`.
  - **Layer:** Go (string-contains is enough for a 5-line file).
  - **File path:** `tests/e2e/phase-0/monorepo-scaffold/workspace_test.go`
  - **Setup it needs:** `repoRoot(t)`; `os.ReadFile`. No YAML parser dep needed.
  - **Helpers it can reuse:** `repoRoot(t)`.

- **AC-3 — Root `package.json` exposes `dev`, `build`, `test`.**
  - **What to assert:** `<root>/package.json` parses; `.scripts` contains keys `dev`, `build`, `test`, each a non-empty string.
  - **Layer:** Go (static check).
  - **File path:** `tests/e2e/phase-0/monorepo-scaffold/package_json_test.go`
  - **Setup it needs:** `encoding/json` decode of `package.json`.
  - **Helpers it can reuse:** `repoRoot(t)`.

- **AC-4 — `pnpm install` from a clean clone succeeds.**
  - **What to assert:** `pnpm install --frozen-lockfile` exits 0 in a clean checkout.
  - **Layer:** Go (wraps `os/exec`).
  - **File path:** `tests/e2e/phase-0/monorepo-scaffold/pnpm_install_test.go`
  - **Setup it needs:** `git clone --local <root> <tmp>` to get a clean tree without test artifacts; `pnpm install --frozen-lockfile` in that tmp; `t.Skip` if `pnpm` or `git` is not on `PATH` so the test is non-fatal on bare runners.
  - **Helpers it can reuse:** `repoRoot(t)`.

- **AC-5 — `dev`, `build`, `test` scripts execute without "missing script" errors.**
  - **What to assert:** `pnpm run build` exits 0. `pnpm run test` is intentionally not run here because it invokes the full smoke flow (covered by `phase-0/smoke-test`); a duplicate would burn CI time. For `dev`, which is a long-running watcher, asserting it can be invoked is enough — start it, give it a 2s budget, then SIGTERM and require the exit code be 0 or 143/SIGTERM.
  - **Layer:** Go.
  - **File path:** `tests/e2e/phase-0/monorepo-scaffold/scripts_run_test.go`
  - **Setup it needs:** Run inside the cloned tmp from AC-4 (or re-clone) to avoid leaking work-tree state; `t.Skip` on missing `pnpm`.
  - **Helpers it can reuse:** the cloned tmp from AC-4 if structured as a single-file test.

### Partial / suspect coverage

(None — `tests/e2e/` does not exist yet.)

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern for booting `apps/server` in a Go test: it builds the binary in `t.TempDir()`, picks a free port via `net.Listen("tcp", "127.0.0.1:0")`, generates a random `CHAT_JWT_SECRET` and `CHAT_INVITE_CODE` via `crypto/rand`. The first E2E test for any feature should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and the `runningServer` struct verbatim into a sibling `harness_test.go` in the per-feature dir. Do not import them — that test's package is `server_ws_hub_test`, the helpers are intentionally local. For this feature only `repoRoot(t)` is needed; the server-boot helpers are out of scope.

## Recommendations for /test-implement

- Create `tests/e2e/phase-0/monorepo-scaffold/harness_test.go` with just `repoRoot(t)` copied from the gold-standard harness.
- Add `tests/e2e/phase-0/monorepo-scaffold/gomod_test.go` with `TestAC1_MonorepoScaffold_SingleRootGoMod`.
- Add `tests/e2e/phase-0/monorepo-scaffold/workspace_test.go` with `TestAC2_MonorepoScaffold_PnpmWorkspaceDeclaration`.
- Add `tests/e2e/phase-0/monorepo-scaffold/package_json_test.go` with `TestAC3_MonorepoScaffold_RootScripts`.
- Add `tests/e2e/phase-0/monorepo-scaffold/pnpm_install_test.go` with `TestAC4_MonorepoScaffold_PnpmInstall` — `t.Skip` if `pnpm` or `git` not on `PATH`.
- Add `tests/e2e/phase-0/monorepo-scaffold/scripts_run_test.go` with `TestAC5_MonorepoScaffold_ScriptsExecute` — `t.Skip` if `pnpm` not on `PATH`; SIGTERM the `dev` watcher after 2s.
