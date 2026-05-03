---
feature: file-perms-and-headers
phase: phase-1
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 3
covered: 0
partial: 0
missing: 3
deferred: 0
---

# E2E test analysis: SQLite file permissions and response security headers

**Spec:** `specs/plans/phase-1/feature-file-perms-and-headers.md`
**Implementation status:** implemented — `EnsureFile` in `apps/server/internal/db/perms.go` enforces 0600 on the SQLite file (with a unix-specific impl in `perms_unix.go`); `db.Open` in `apps/server/internal/db/open.go` calls it. `SecurityHeaders` middleware in `apps/server/internal/http/headers_middleware.go` is wired as the outermost wrap at `apps/server/main.go:174`.
**E2E test directory:** `tests/e2e/phase-1/file-perms-and-headers/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | The SQLite database file is created with mode `0600` (owner read/write only). | missing | — |
| AC-2 | HTTP responses include `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` on all routes. | missing | — |
| AC-3 | Headers are present on both 2xx and error responses. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — SQLite file mode is 0600 after first boot**
- **What to assert:** Boot the binary against a fresh `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Wait for the port to listen (proves boot completed). `os.Stat(<path>)` → assert `info.Mode().Perm() == 0o600`. On non-unix platforms the perm bit interpretation differs; gate the assertion with `if runtime.GOOS == "windows" { t.Skip("0600 perm mode is unix-specific") }`. Also assert the parent directory is owner-only (the `EnsureFile` impl in `perms.go` may or may not chmod the dir; verify by reading the file once and only assert on the file itself if so). Repeat with the file pre-existing in mode 0644 → after boot, mode is 0600 (proves `EnsureFile` chmods on every boot, not just first creation).
- **Layer:** Go (boot binary, file stat).
- **File path:** `tests/e2e/phase-1/file-perms-and-headers/sqlite_file_mode_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard.

**AC-2 — four SEC-10 headers on every response**
- **What to assert:** Boot the server. Hit a representative set of routes and assert each response carries exactly the four headers with the documented values:
  - `GET /debug/subs?channel=%23general` (200)
  - `POST /api/auth/register` (likely 4xx without proper body)
  - `GET /api/auth/me` without Authorization (401)
  - `GET /nope` (404)
  For each response, assert: `Content-Security-Policy` is non-empty (the spec says "restrictive policy"; verify exact value by reading `headers_middleware.go` once during test authoring and pin to that string), `X-Content-Type-Options == "nosniff"`, `Referrer-Policy == "no-referrer"`, `X-Frame-Options == "DENY"`. Also dial `/ws` and inspect the upgrade response (`websocket.Dial` returns the `*http.Response` of the handshake) — assert the same four headers are present on the 101 response.
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/file-perms-and-headers/security_headers_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `requireSecHeaders(t, h http.Header)` to dedup the four assertions.

**AC-3 — headers present on 2xx and error responses**
- **What to assert:** Subset of AC-2 with the failure-path framing pinned. Construct one 2xx response (`GET /debug/subs?channel=%23general`) and one error response per code class:
  - 400 — `POST /api/auth/login` with a non-JSON body (e.g. `garbage`).
  - 401 — `GET /api/auth/me` with no Authorization.
  - 404 — `GET /nope`.
  - 405 — `GET /api/auth/login` (login is POST-only).
  - 413 — `POST /api/auth/register` with a 16385-byte body (BodyCap fires).
  - 500 — only assertable with the panic probe build tag (see `access-log-fields-and-wiring`); `t.Skip` if absent.
  For each response, run `requireSecHeaders(t, resp.Header)`. The test proves the outer-most ordering of `SecurityHeaders` works: even when inner middleware (`BodyCap`, `Recover`) writes the response, the outer wrap has already set the headers.
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/file-perms-and-headers/headers_on_errors_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `requireSecHeaders(t, h)`.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/file-perms-and-headers/harness_test.go` with copied helpers + `requireSecHeaders(t, h http.Header)` that asserts the four header keys with values pinned from `headers_middleware.go`.
- Add `sqlite_file_mode_test.go` (AC-1, skip on Windows), `security_headers_test.go` (AC-2), `headers_on_errors_test.go` (AC-3).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
