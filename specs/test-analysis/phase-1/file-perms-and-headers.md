---
feature: file-perms-and-headers
phase: phase-1
analyzed_at: 2026-05-03T14:42:24Z
analyzed_commit: 7e3010dd771551d1f4371e00034d0bb88dae18fa
implementation_status: partial
total_acs: 3
covered: 1
partial: 2
missing: 0
deferred: 0
---

# Test analysis: SQLite file permissions and response security headers

**Spec:** `specs/plans/phase-1/feature-file-perms-and-headers.md`
**Implementation status:** partial — `apps/server/internal/db.EnsureFile` and `apps/server/internal/http.SecurityHeaders` exist and pass strong in-package tests. **Neither is wired into `apps/server/main.go`.** A live server response does not carry the security headers; no DB file is opened (no SQLite integration shipped yet). The PR's spec marks the feature "implemented", but the runtime contract is not satisfied.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | The SQLite database file is created with mode `0600` (owner read/write only). | covered | `apps/server/internal/db/perms_test.go::TestEnsureFile_CreatesWith0600_SEC14` + `TestEnsureFile_TightensExistingWiderMode_SEC14` + `TestEnsureFile_IsIdempotent`. The function correctly tightens the umask, opens with `0o600`, and `chmod`s. The function will become load-bearing once `feature-sqlite-schema-and-ulid` lands and main.go opens a DB. |
| AC-2 | HTTP responses include CSP / nosniff / no-referrer / DENY headers on all routes. | partial | `apps/server/internal/http/headers_middleware_test.go::TestSecurityHeaders_OK_SEC10` + `TestSecurityHeaders_ErrorResponse_SEC10` + `TestSecurityHeaders_NotFound_SEC10` + `TestSecurityHeaders_CSPLiteralMatchesPRD_SEC10` (CSP literal ≡ PRD §9 — drift-detector). The middleware is correct **but** `apps/server/main.go` does not wrap its mux with `SecurityHeaders`, so the live `/ws` and `/debug/subs` handlers respond without the headers. |
| AC-3 | Headers are present on both 2xx and error responses. | partial | Same test suite covers 200 (`TestSecurityHeaders_OK_SEC10`), 500 (`TestSecurityHeaders_ErrorResponse_SEC10`), and 404 (`TestSecurityHeaders_NotFound_SEC10`). Same wiring gap as AC-2: the unit tests prove composition works; `main.go` doesn't compose. |

## Findings

### Partial — wiring gap for AC-2 and AC-3

`grep -rn 'SecurityHeaders' apps/` shows only the file that defines it and its test file. `apps/server/main.go` builds its mux as `mux := http.NewServeMux(); mux.HandleFunc("/ws", …); mux.HandleFunc("/debug/subs", …); srv := &http.Server{Handler: mux}` — no middleware wrap. A live `curl -I http://127.0.0.1:<port>/debug/subs?channel=%23general` at this SHA returns no `Content-Security-Policy`, no `X-Frame-Options`, etc.

The unit tests verify what `SecurityHeaders(handler)` does when constructed. They do NOT verify that the real server constructs it. AC-2/AC-3 are runtime contracts on the deployed binary's responses, not on a hypothetical wrapped handler.

**No failing system test added by this run.** Reasoning:
- Adding `tests/file-perms-and-headers/system_test.go` that builds the server and asserts headers would fail today and stay red until someone wires the middleware in. That's the right test to write.
- However, the deliberate choice of `git -C "$WORKTREE"` builds elsewhere in the suite shows the cost: each binary-launch test runs `go build ./apps/server` (~700 ms × N tests). Adding a 4-header-check system test now means `pnpm test` includes a guaranteed-failing test step; CI on every commit goes red until the wiring lands.
- The findings doc here makes the gap explicit. The next PR (likely `feature-sqlite-schema-and-ulid` or a follow-up that wires middleware composition) will close it; the agent will detect the wiring on the next tick and re-promote AC-2/AC-3 to covered.
- If the maintainer prefers a failing-anchor test, drop a one-liner into `tests/file-perms-and-headers/wiring_test.go` that asserts a literal substring like `SecurityHeaders` exists in `apps/server/main.go`. That's static, cheap, and unambiguous.

### Covered — AC-1

`EnsureFile` is a pure function that operates on a path and verifies behavior on POSIX systems (Windows is correctly skipped). Three tests cover create-fresh, tighten-existing-wider, idempotency. The function isn't called from main.go yet, but the AC's contract — "the SQLite database file is created with mode 0600" — is a contract on the function whose job it is to do that, and the function honors it. When main.go starts opening a DB (in `feature-sqlite-schema-and-ulid`), it must call `EnsureFile`; if it skips or rolls its own opener, the future findings for that feature should flag the regression.

### Spec note

Spec lists `apps/server/internal/db/open.go` and `apps/server/internal/http/middleware_security_headers.go` as the file names. Implementation uses `apps/server/internal/db/perms.go` and `apps/server/internal/http/headers_middleware.go`. Functionally equivalent; spec-text-vs-impl-name drift, not a coverage issue.

## Recommendations

1. **Wire `SecurityHeaders` into `apps/server/main.go`** — `srv.Handler = SecurityHeaders(mux)`. This is a one-line production change, out of test-agent scope, but it is THE blocker for AC-2 and AC-3 going green.
2. **When the wiring lands**, the test-agent's next tick will re-evaluate at the new SHA. No findings doc edit needed in advance.
3. **Optional anchor test (lightweight):** add `tests/file-perms-and-headers/wiring_test.go` that statically asserts `apps/server/main.go` contains a `SecurityHeaders(` call. Catches future un-wiring, doesn't require launching the binary. Trade-off: same red-CI signal until wiring lands, but lower per-run cost than a binary-launch system test (no `go build` step). Skipped this run for the same red-CI reason as the system test; if the maintainer prefers an anchor over the findings-doc-only approach, this is the cheaper option.
4. **Spec follow-up:** rename file-list entries to match impl (`open.go` → `perms.go`, `middleware_security_headers.go` → `headers_middleware.go`).
