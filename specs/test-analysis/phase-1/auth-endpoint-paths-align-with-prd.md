---
feature: auth-endpoint-paths-align-with-prd
phase: phase-1
analyzed_at: 2026-05-03T16:08:49Z
analyzed_commit: ec3e7a102b6783dec7870e4032f4fb7a9babdc60
implementation_status: stub
total_acs: 4
covered: 0
partial: 0
missing: 0
deferred: 4
---

# Test analysis: Align auth endpoint paths with PRD §10

**Spec:** `specs/plans/phase-1/feature-auth-endpoint-paths-align-with-prd.md`
**Implementation status:** stub — spec landed (status: planned), no code change. Confirmed at this SHA: `apps/server/main.go:106-110` still mounts `/api/{register,login,me,logout,ws-ticket}`. The PRD §10-aligned `/api/auth/<verb>` routes do not exist; a `curl http://localhost:PORT/api/auth/login` would return 404.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | The server registers handlers at `/api/auth/register`, `/api/auth/login`, `/api/auth/me`, `/api/auth/logout`, `/api/auth/ws-ticket` (matching PRD §10). | deferred | impl is stub — main.go still uses `/api/<verb>` for all five. |
| AC-2 | Existing tests (`auth_handlers_test.go`) and `scripts/smoke.sh` are updated to use the new paths and continue to pass. | deferred | impl is stub. The current tests/script use `/api/<verb>` and pass; they'll need a coordinated path-rename. |
| AC-3 | No other behavioural change — request/response shapes, JWT semantics, ticket TTL, audit-log entries all remain as PR #38 implemented them. This plan is path-only. | deferred | impl is stub; this AC is a *constraint* on the future PR rather than a checkable contract today. The agent will verify post-implementation that no `auth_events` kinds, JWT claim names, or envelope codes shifted. |
| AC-4 | Either the parent plan (`feature-auth-endpoints.md`) is amended to reflect `/api/auth/*` paths, or it is left alone with a one-line note that the PRD wins. Either is acceptable; not aligning the parent plan would be misleading. | deferred | impl is stub. `specs/plans/phase-1/feature-auth-endpoints.md` still lists `/api/<verb>` paths in its AC text. |

## Findings

### Why "stub" rather than "missing"

This spec is a deliberate spec-vs-impl reconciliation discovered after PR #38 merged. The implementer correctly followed the parent plan; the parent plan diverged from the PRD on path naming. PR #49 landed this plan as `**Status:** planned` to track the gap; the actual rename is a separate PR.

A path rename touches 5 lines in main.go, the test file's `switch path` block, and the smoke script's three curl invocations. Mechanically simple; it's just unshipped today.

### Why this is worth tracking as a separate feature instead of folding into auth-endpoints

The two specs interact cleanly: `feature-auth-endpoints` (PR #38) is `done` for behavior, and this spec (PR #49) tracks the path realignment. Marking auth-endpoints partial again (because the paths don't match the PRD) would lose the signal that the auth-handler code itself is correct. A separate stub feature with `deferred` ACs keeps the audit clear: "the handlers work, the paths need to move."

### Cross-feature observations

- **`feature-auth-endpoints` AC-1 through AC-5** stay covered against the implemented paths. The agent's previous findings (PR #50) used the AC text verbatim, which says `POST /api/register` etc. — that's what the implementation registered, what the tests exercise, and what `scripts/smoke.sh` calls. So those ACs are honestly satisfied as written. PRD §10 is what diverges.
- **`feature-channels-and-messages`** does not face the same /api/auth/ vs /api/ question — the PRD pins `POST /api/channels` and `GET /api/channels/{id}/messages` (no `/auth/` prefix), and the spec + impl both use those. The wiring gap there is a separate issue (PR #55 findings).
- **Phase 2 client packages (`feature-go-client-package`, `feature-ts-api-client-package`)** will pin path strings. Aligning before they materialize avoids a coordinated path-rename across the server + two clients later. This is the spec body's stated motivation; flagging it here so the agent's index reflects the urgency.

### What changes when the impl PR lands

- This feature: 0/4 deferred → 4/4 covered (or 3/4 + 1 partial if the parent plan isn't updated, since AC-4 has the "either is acceptable" branch).
- `feature-auth-endpoints`: ACs reword from `/api/<verb>` to `/api/auth/<verb>` in the findings doc, but coverage stays 7/7. The test references stay the same — only the URL string in test-fixture setup changes.
- `scripts/smoke.sh` continues to exit 0; the wiring vitest's structural assertions don't pin URL strings, so they're robust to this rename.

## Recommendations

1. **No tests added by this run** — there is no implementation to anchor tests against, and writing a guaranteed-failing test for a planned-only spec would put `pnpm test` in a permanent-fail state. Same call as PR #37 / PR #47 / PR #52.
2. When the implementation PR lands:
   - Verify `apps/server/main.go` registers all five `/api/auth/<verb>` routes.
   - Verify `apps/server/internal/http/auth_handlers_test.go` and `scripts/smoke.sh` were both updated in the same PR (no path-mismatch flake during the rename).
   - Re-evaluate `phase-1/auth-endpoints` findings: AC text in the per-AC table should read `/api/auth/<verb>`, but coverage stays 7/7.
3. **Cross-feature note:** when the next test-watch tick runs after the rename PR merges, consider grepping `tests/` for any hard-coded `/api/<verb>` paths the agent may have anchored. Today there are none, but the system-layer `tests/` dir would need a sweep.
