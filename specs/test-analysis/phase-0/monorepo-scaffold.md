---
feature: monorepo-scaffold
phase: phase-0
analyzed_at: 2026-05-03T15:15:00+02:00
analyzed_commit: 206b9e265fadf27b7b59cf0f99e7db941231676a
implementation_status: implemented
total_acs: 5
covered: 3
partial: 0
missing: 2
deferred: 0
---

# Test analysis: Monorepo scaffold

**Spec:** `specs/plans/phase-0/feature-monorepo-scaffold.md`
**Implementation status:** implemented — `go.mod`, `pnpm-workspace.yaml`, `package.json`, and `.gitignore` all exist and the `pnpm` workspace + Go module both build.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages. | covered (with design drift) | `tests/scaffold/go_module_test.go::TestAC1_RootGoModuleIsSingleAndNamedHackathon` |
| AC-2 | `pnpm-workspace.yaml` declares `apps/*` and `packages/*` workspaces. | covered | `tests/scaffold/pnpm_workspace_test.ts` |
| AC-3 | Root `package.json` exposes `dev`, `build`, and `test` scripts that fan out to the relevant apps/packages. | covered | `tests/scaffold/root-package-json.test.ts` |
| AC-4 | Running `pnpm install` from a clean clone succeeds without errors. | missing | — |
| AC-5 | Running each top-level script (`dev`, `build`, `test`) completes without configuration errors (script bodies may be stubs at this stage). | missing | — |

## Findings

### Design drift on AC-1
The spec says `go.work` should define multiple workspaces. The implementation deliberately diverges: a single root `go.mod` with module name `hackathon`. The decision is recorded in `CLAUDE.md` ("Single root `go.mod` with module name `hackathon`. There is no `go.work` and no per-app `go.mod`."). The existing test `TestAC1_RootGoModuleIsSingleAndNamedHackathon` enforces the *new* design (no `go.work`, no per-app `go.mod`). This is "covered" in the sense that the AC's intent — "Go workspace is set up correctly" — is verified, but the spec text needs an update to match reality. Recommendation: update `feature-monorepo-scaffold.md` AC-1 to read "single root `go.mod` named `hackathon` with no `go.work` and no per-app `go.mod`."

### Missing tests

**AC-4 — `pnpm install` succeeds.**
A meaningful proxy: assert `pnpm-lock.yaml` exists, parses as valid YAML, and references each workspace package. Live `pnpm install --frozen-lockfile` is already verified by CI (`.github/workflows/ci.yml` job `pnpm`); duplicating it as a vitest would be slow. Layer: vitest. Location: `tests/monorepo-scaffold/install.test.ts`.

**AC-5 — top-level scripts run without configuration errors.**
Static check: `package.json` `scripts.{dev,build,test}` exist, are non-empty, and contain `pnpm -r` (already partially covered by `root-package-json.test.ts::AC-3`, but AC-5 specifically targets *running* them without error). Live verification — spawning each script — would dominate suite runtime; CI already exercises `build` and `test`. Recommended: add a vitest that spawns `pnpm -r --if-present <script> --help` (cheap dry-run) for each of `dev`, `build`, `test` and asserts exit 0. Layer: vitest. Location: `tests/monorepo-scaffold/scripts.test.ts`.

## Recommendations

1. Add `tests/monorepo-scaffold/install.test.ts` — `pnpm-lock.yaml` exists and references workspace packages.
2. Add `tests/monorepo-scaffold/scripts.test.ts` — top-level scripts spawn without configuration errors.
3. Update spec AC-1 to match the single-root-`go.mod` decision; remove references to `go.work`.
