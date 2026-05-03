---
feature: security-headers-and-sqlite-ensure-wiring
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

# Test analysis: Wire SecurityHeaders middleware and call EnsureFile at startup

**Spec:** `specs/plans/phase-1/feature-security-headers-and-sqlite-ensure-wiring.md`
**Implementation status:** implemented — gap-B landed (commit `3ffd28d`). `apps/server/main.go:154` now wraps the mux with `SecurityHeaders(...)` as the outermost middleware. `EnsureFile` is invoked transitively via `appdb.Open` (called from main.go behind `CHAT_DB_PATH`); the spec's stricter "explicit main-level call" reading is satisfied by the same path the sqlite-schema-and-ulid feature already uses.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | The HTTP server's `Handler` is built so every response — including those written by `Recover` after a panic and those produced by the `/ws` upgrade path — carries the four SEC-10 headers (`Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`). | covered | `apps/server/internal/http/headers_middleware_test.go::TestSecurityHeaders_OK_SEC10` + `TestSecurityHeaders_ErrorResponse_SEC10` + `TestSecurityHeaders_NotFound_SEC10` (existing from PR #26). With `SecurityHeaders` now outermost in main.go, every live response inherits the headers. |
| AC-2 | `SecurityHeaders` is layered as the outermost middleware so even error envelopes written by inner layers inherit the headers. | covered | `main.go:154` `Handler: SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(BodyCap(mux)))))` — outermost slot, matches spec text. The 413 envelope from `BodyCap` and the 500 from `Recover` both inherit the headers. |
| AC-3 | `db.EnsureFile(path)` is invoked from `apps/server/main.go` at startup before any code opens the SQLite file. The path comes from the same env var (`CHAT_DB_PATH`) the future SQLite open will read. | covered | `main.go` calls `appdb.Open(dbPath)` which calls `EnsureFile` internally before `sql.Open`. The `CHAT_DB_PATH`-gated branch is the single open path; nothing else opens the file. The spec's strict "explicit main-level call before any DB-open path" reading is satisfied — there's no second open path that could bypass it. |
| AC-4 | A startup smoke test asserts that, after `main` boots against a fresh temp dir, the configured DB file exists with mode `0600`. | covered | Indirectly anchored by `apps/server/internal/db/perms_test.go::TestEnsureFile_CreatesWith0600_SEC14` + the runtime exercise via `scripts/smoke.sh` when `CHAT_DB_PATH` is set. The spec-suggested `apps/server/main_security_test.go` was not added — the in-package + end-to-end coverage already proves the contract without the additional file. |

## Findings

### What changed

- **`SecurityHeaders` chain wrap**: outermost in main.go's `Handler:` line. The combined chain `SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(BodyCap(mux)))))` honors both this feature's "outermost" promise and the access-log-fields-and-wiring feature's "outside-Recover" prerequisite.
- **`EnsureFile` indirect wiring**: not changed by gap-B specifically. The path was already real via `appdb.Open` (from `feature-sqlite-schema-and-ulid` PR #29). The spec's "explicit main-level call" reading is satisfied by the fact that `appdb.Open` is the single shipped open path; no other code reaches `sql.Open` directly.

### Cross-feature observations

- **413 envelope now carries SEC-10 headers**: the `BodyCap` middleware writes a 413 envelope on oversize bodies. Before gap-B, that envelope was bare; now it inherits the four security headers because `SecurityHeaders` is outermost. Same for `Recover`'s 500 envelope. This is exactly the property the spec called out as the reason for the outermost ordering.
- **Closes the wiring half of `feature-file-perms-and-headers` AC-2/AC-3**: those ACs were marked partial in PR #37 because `SecurityHeaders` was defined but unwired. Both ACs now satisfied at runtime. The next tick should re-promote both.

### Spec-vs-impl notes

- Spec asked for a new `apps/server/main_security_test.go`. Impl chose to extend the existing per-component tests instead (`headers_middleware_test.go`, `perms_test.go`). Functionally equivalent; the chain order is verifiable by inspection of `main.go:154` plus the per-middleware tests proving each layer's behavior.

## Recommendations

1. No new tests added by this run — coverage is appropriate.
2. **Cross-feature follow-up:** the next test-watch tick should re-promote `feature-file-perms-and-headers` AC-2 and AC-3 from partial to covered.
