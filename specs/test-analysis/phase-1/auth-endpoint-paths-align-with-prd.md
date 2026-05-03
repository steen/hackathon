---
feature: auth-endpoint-paths-align-with-prd
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 4
covered: 4
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Align auth endpoint paths with PRD §10

**Spec:** `specs/plans/phase-1/feature-auth-endpoint-paths-align-with-prd.md`
**Implementation status:** implemented — gap-C landed (commit `6eb9b36`). All five auth routes now mount under `/api/auth/<verb>`. Test fixtures, smoke script, and the parent feature's spec text all updated in the same change.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | The server registers handlers at `/api/auth/register`, `/api/auth/login`, `/api/auth/me`, `/api/auth/logout`, `/api/auth/ws-ticket` (matching PRD §10). | covered | `apps/server/main.go:119-123` registers all five `/api/auth/<verb>` routes verbatim. The legacy `/api/<verb>` mounts are gone. |
| AC-2 | Existing tests (`auth_handlers_test.go`) and `scripts/smoke.sh` are updated to use the new paths and continue to pass. | covered | `auth_handlers_test.go` switch block at lines 96-104 uses `/api/auth/<verb>`; `scripts/smoke.sh:129/139/152` calls the new paths. Verified at this SHA: `go test ./apps/server/internal/http/...` and `bash scripts/smoke.sh` both pass. |
| AC-3 | No other behavioural change — request/response shapes, JWT semantics, ticket TTL, audit-log entries all remain as PR #38 implemented them. | covered | `auth_events.kind` constants unchanged (`AuthEventLoginSuccess` etc. at `auth_handlers.go:23-28`); JWT `Claims.tv` unchanged; ticket store TTL constant `30 * time.Second` unchanged. The `feature-auth-endpoints` test suite (25+ tests) still passes byte-identically against the renamed routes. |
| AC-4 | Either the parent plan (`feature-auth-endpoints.md`) is amended to reflect `/api/auth/*` paths, or it is left alone with a one-line note that the PRD wins. Either is acceptable; not aligning the parent plan would be misleading. | covered | Parent spec's AC text was updated in the same commit (lines 13-17 of `feature-auth-endpoints.md`) to read `POST /api/auth/register` etc. — verified by the diff. The parent spec is now self-consistent with the impl. |

## Findings

### What changed

- **5-line route rename** in `apps/server/main.go` (lines 119-123).
- **Test fixture paths** updated in `auth_handlers_test.go` (the `switch path` block at line 96).
- **Smoke script paths** updated in `scripts/smoke.sh` (3 curl invocations: register/login/ws-ticket).
- **Parent spec realignment** in `feature-auth-endpoints.md` AC text.

Nothing else changed. The implementation followed the spec's "path-only" constraint exactly.

### Cross-feature observations

- **Phase-2 client packages now have a stable URL contract** to pin against. `feature-go-client-package`, `feature-ts-api-client-package`, and `feature-cli-full-commands` all reference `/api/auth/<verb>` paths consistently with the PRD; they can be implemented without a coordinated server-rename later.
- **The `feature-auth-endpoints` parent feature stays at 7/7 covered.** AC text refreshed; test references stay the same since the per-AC table only names test functions, not URL strings.

## Recommendations

1. No new tests added by this run — coverage is appropriate.
2. **Cross-feature note:** when the next tick analyzes `feature-auth-endpoints`, the AC text in the per-AC table should be refreshed from `/api/<verb>` to `/api/auth/<verb>` to match the parent spec. Coverage stays 7/7; only the URL strings in the test-reference column change.
