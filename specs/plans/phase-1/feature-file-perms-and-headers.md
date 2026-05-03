# Feature: SQLite file permissions and response security headers

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Requirements covered
- (security hardening; cross-cutting baseline for hosting per US-10 single-binary deploy)

## Acceptance criteria
- The SQLite database file is created with mode `0600` (owner read/write only).
- HTTP responses include the following headers on all routes:
  - `Content-Security-Policy` — restrictive policy suitable for the embedded web app.
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
  - `X-Frame-Options: DENY`
- Headers are present on both 2xx and error responses.

## Implementation steps
1. Open the SQLite file via `os.OpenFile(path, O_RDWR|O_CREATE, 0600)` (or set umask before connection) so a freshly created file has mode `0600`. Verify with `os.Stat`.
2. Create `apps/server/internal/http/middleware_security_headers.go` that sets the four headers on every response.
3. Wire the middleware as the outermost layer so it applies to error responses too.
4. Document the chosen CSP in code comments (one short line; see `feature-logging-and-error-envelope.md` for envelope coordination).

## Test plan
- `test_sqlite_file_created_with_0600` — covers SEC file perms.
- `test_response_includes_csp_header` — covers SEC headers.
- `test_response_includes_nosniff_no_referrer_frame_deny` — covers SEC headers.
- `test_error_responses_also_carry_security_headers` — covers SEC headers.

## Files expected to be touched or created
- `apps/server/internal/db/open.go`
- `apps/server/internal/db/open_test.go`
- `apps/server/internal/http/middleware_security_headers.go`
- `apps/server/internal/http/middleware_security_headers_test.go`

## Risks
- A too-restrictive CSP can break the embedded web app in Phase 3; mitigated by aligning the CSP with the embedded build's known asset origin.
