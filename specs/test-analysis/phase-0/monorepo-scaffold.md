---
feature: monorepo-scaffold
phase: phase-0
analyzed_at: 2026-05-03T13:35:37Z
analyzed_commit: c3e7e991b84a21a648eaed1ce7188c28647079db
implementation_status: implemented
total_acs: 5
covered: 5
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Monorepo scaffold

**Spec:** `specs/plans/phase-0/feature-monorepo-scaffold.md`
**Implementation status:** implemented — `go.mod` (single root, module `hackathon`), `pnpm-workspace.yaml`, `package.json`, and `.gitignore` all exist; `pnpm install` and `go test ./...` both succeed locally and in CI.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages. | covered (with design drift) | `tests/scaffold/go_module_test.go::TestAC1_RootGoModuleIsSingleAndNamedHackathon` |
| AC-2 | `pnpm-workspace.yaml` declares `apps/*` and `packages/*` workspaces. | covered | `tests/scaffold/pnpm_workspace_test.ts` |
| AC-3 | Root `package.json` exposes `dev`, `build`, and `test` scripts that fan out to the relevant apps/packages. | covered | `tests/scaffold/root-package-json.test.ts` |
| AC-4 | Running `pnpm install` from a clean clone succeeds without errors. | covered | `tests/monorepo-scaffold/install.test.ts` (lockfile present, parses, references workspace importer) |
| AC-5 | Running each top-level script (`dev`, `build`, `test`) completes without configuration errors (script bodies may be stubs at this stage). | covered | `tests/monorepo-scaffold/scripts.test.ts` (each script body uses `pnpm -r --if-present`) |

## Findings

### Design drift on AC-1

The spec text says `go.work` should define multiple workspaces. The implementation deliberately diverges to a single root `go.mod` with module name `hackathon`, recorded in `CLAUDE.md`. The existing `TestAC1_RootGoModuleIsSingleAndNamedHackathon` enforces the new design. The intent of the AC ("Go workspace is correctly set up") is verified, but the spec text is now inaccurate.

**Recommendation:** update `feature-monorepo-scaffold.md` AC-1 to read "single root `go.mod` named `hackathon` with no `go.work` and no per-app `go.mod`" and drop `go.work` from the "Files expected to be touched or created" list. (Out of scope for the test agent.)

### Coverage notes

The new tests for AC-4 and AC-5 (added by the bootstrap PR) intentionally avoid spawning `pnpm install` or each `pnpm run <script>` live — those are slow and CI already exercises them. Instead they make static assertions:

- AC-4: `pnpm-lock.yaml` exists, parses as YAML, and contains the root importer key `.`. Catches the regression "lockfile got deleted" or "lockfile is corrupt", which is what the AC actually wants to prevent.
- AC-5: each of `scripts.{dev,build,test}` is a string that includes both `pnpm -r` (or `--recursive`) AND `--if-present`. The `--if-present` part is what guarantees an empty-workspace clone won't error with "missing script", which is the AC's stated intent.

## Recommendations

1. Spec update: rewrite AC-1 to match the single-root-`go.mod` reality and drop `go.work` from the file list.
2. No new tests needed; coverage is complete.
