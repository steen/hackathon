# Feature: Monorepo scaffold

**Parent phase:** [Phase 0: Walking skeleton, system test ready](../phase-0-walking-skeleton-system-test-ready.md)
**Status:** planned

## Requirements covered
- (no user-story IDs from the PRD map to scaffolding; this is foundational infrastructure for all subsequent features)

## Acceptance criteria
- `go.work` defines workspaces for `apps/server`, `apps/cli`, and shared Go packages.
- `pnpm-workspace.yaml` declares `apps/*` and `packages/*` workspaces.
- Root `package.json` exposes `dev`, `build`, and `test` scripts that fan out to the relevant apps/packages.
- Running `pnpm install` from a clean clone succeeds without errors.
- Running each top-level script (`dev`, `build`, `test`) completes without configuration errors (script bodies may be stubs at this stage).

## Implementation steps
1. Create `go.work` at repo root and add module entries for the Go apps that will be created in this phase.
2. Create `pnpm-workspace.yaml` listing `apps/*` and `packages/*`.
3. Create root `package.json` with `name`, `private: true`, and `scripts` for `dev`, `build`, `test`. Each script invokes the equivalent across workspaces (e.g., `pnpm -r run build`).
4. Add `.gitignore` entries for Go and Node build artifacts (`*.exe`, `node_modules`, `dist`, etc.) if not already present.
5. Verify `pnpm install` runs cleanly.

## Test plan
- Manual: `pnpm install` from a clean clone exits 0.
- Manual: `pnpm run build` and `pnpm run test` execute without "missing script" errors (even if the underlying workspaces are empty stubs).

## Files expected to be touched or created
- `go.work`
- `pnpm-workspace.yaml`
- `package.json`
- `.gitignore`

## Risks
- None identified.
