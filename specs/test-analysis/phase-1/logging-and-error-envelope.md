---
feature: logging-and-error-envelope
phase: phase-1
analyzed_at: 2026-05-03T19:11:26Z
analyzed_commit: f2d750de9dbdf5b20e48b4a226633bcac3127fec
implementation_status: implemented
total_acs: 4
covered: 0
partial: 0
missing: 4
deferred: 0
---

# E2E test analysis: Access-log middleware and user-safe error envelope

**Spec:** `specs/plans/phase-1/feature-logging-and-error-envelope.md`
**Implementation status:** implemented — `AccessLog` (`apps/server/internal/http/middleware.go`) writes structured log lines, redacting `token`/`ticket` from the URL. `Envelope`, `WriteOK`, `WriteError`, and the `ErrorBody{code,message}` payload live in `apps/server/internal/http/errors.go`. `Recover` (`middleware.go`) catches panics and emits the envelope. The chain is wired at `apps/server/main.go:174`.
**E2E test directory:** `tests/e2e/phase-1/logging-and-error-envelope/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | Access-log middleware logs method, path, status, latency, IP, and user ID (if known). | missing | — |
| AC-2 | Sensitive query parameters (`token`, `ticket`) are stripped/redacted from logged URLs. | missing | — |
| AC-3 | Every JSON response uses the envelope `{ ok: bool, data: any|null, error: { code: string, message: string }|null }` per PRD §6. `ok=false` implies `error` is non-null and `data` is null; `ok=true` implies the inverse. | missing | — |
| AC-4 | Internal error details (stack, raw DB error) are not exposed to clients but are logged on the server side with a request ID. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — access-log fields present**
- **What to assert:** Boot the server with stderr captured into a thread-safe `bytes.Buffer`. Register + login a user; `GET /api/auth/me` with bearer; `GET /debug/subs?channel=%23general` (anonymous). Scan the captured log buffer for one line per request. Each line must contain key=value pairs for `method`, `path`, `status`, `latency_ms` (or whatever key `middleware.go` emits — verify by reading once during test authoring), `remote_ip`, and `user_id` (empty for the anonymous request, populated for the `/api/auth/me` request). Match by request id (`X-Request-Id` response header → `request_id=<uuid>` log substring).
- **Layer:** Go (boot binary, HTTP, log capture).
- **File path:** `tests/e2e/phase-1/logging-and-error-envelope/access_log_fields_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Replace `cmd.Stdout = os.Stderr` with a `syncBuf` so the test can scan log lines.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard with a `syncBuf` log capture extension, plus `register`, `login`, `me`, `awaitLogLine`.

**AC-2 — `token` and `ticket` query params redacted in logs**
- **What to assert:** Boot, register + login. Issue a ws-ticket → `tk`. `GET /api/auth/me?token=ABC123XYZ&ticket=DEF456` (with bearer; the query params are decoys). Also dial `/ws?ticket=<tk>`. Scan the log buffer for both requests; assert the recorded `path` (or `url`) does NOT contain the literal substrings `ABC123XYZ`, `DEF456`, or `<tk>`. The redacted form should contain `token=REDACTED` (or whatever `middleware.go` emits — verify the exact placeholder string by reading once) and `ticket=REDACTED`. Test other params are NOT redacted by sending `?channel=%23general&token=secret123` and asserting the log line still contains `channel=%23general` (or `channel=#general` after URL decode) but not `secret123`.
- **Layer:** Go (boot binary, HTTP + WS, log capture).
- **File path:** `tests/e2e/phase-1/logging-and-error-envelope/log_redaction_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `awaitLogLine`.

**AC-3 — every JSON response uses the envelope, with the documented invariants**
- **What to assert:** Hit a representative success and failure surface across the whole API:
  - 200: `GET /debug/subs?channel=%23general` (note: this returns text, not JSON — exclude); use `POST /api/auth/register` with valid body (200/201), `GET /api/auth/me` with bearer (200), `GET /api/channels` with bearer (200), `POST /api/channels` (200/201), `GET .../messages` (200).
  - 4xx: `POST /api/auth/login` with wrong password (401), `POST /api/auth/register` with bad invite (4xx), `GET /api/auth/me` no Authorization (401), `POST /api/channels` duplicate name (409), `GET /api/channels/missing/messages` (404 or 400), `POST /api/auth/register` with body >16 KiB (413).
  For each response with `Content-Type: application/json...`, `json.Unmarshal` into a struct with all three fields as `json.RawMessage` (lets us distinguish `null` from missing). Assert: all three keys present; `ok` is bool; if `ok==true` then `error` is JSON null and `data` is non-null; if `ok==false` then `data` is JSON null and `error` is non-null with both `code` and `message` strings (`code != ""`, `message != ""`).
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/logging-and-error-envelope/envelope_shape_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `requireEnvelope(t, body []byte, wantOK bool)`.

**AC-4 — internal error details not in client body, present in server log with request id**
- **What to assert:** Trigger a server-side error path that has internal detail (e.g. `POST /api/auth/login` with a malformed JSON body, or send a request to a path that the handler chain catches with a non-trivial error). Assert the response body, decoded as the envelope, contains a `message` that is generic (no SQL text — match against denylist substrings: `SELECT`, `INSERT`, `goroutine`, `.go:`, `panic:`, the path of any source file under `apps/server/`). For the panic path: requires the panic-probe build tag (skip otherwise); after the probe panic, scan the log buffer for a line containing the request id from the response's `X-Request-Id` header AND a stack-trace marker (`goroutine ` or `panic:`) AND no client-visible text from the response body. This proves the detail goes server-side, the generic envelope goes client-side.
- **Layer:** Go (boot binary, HTTP, log capture).
- **File path:** `tests/e2e/phase-1/logging-and-error-envelope/internal_detail_redaction_test.go`.
- **Setup it needs:** same as AC-1; panic probe build tag for the panic half.
- **Helpers it can reuse:** harness; `awaitLogLine`.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. Extend `runningServer` with a `logs *syncBuf` field — every test in this feature reads server logs.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/logging-and-error-envelope/harness_test.go` with copied helpers + `syncBuf`, `register`, `login`, `me`, `awaitLogLine(t,srv,substr,timeout)`, `requireEnvelope(t,body,wantOK)`.
- Add `access_log_fields_test.go` (AC-1), `log_redaction_test.go` (AC-2), `envelope_shape_test.go` (AC-3), `internal_detail_redaction_test.go` (AC-4 — `t.Skip` panic half if no probe).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
