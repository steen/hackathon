---
feature: access-log-fields-and-wiring
phase: phase-1
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 4
covered: 0
partial: 0
missing: 4
deferred: 0
---

# E2E test analysis: Access-log field completeness and middleware wiring

**Spec:** `specs/plans/phase-1/feature-access-log-fields-and-wiring.md`
**Implementation status:** implemented — `AccessLog`, `RequestIDMiddleware`, and `Recover` live in `apps/server/internal/http/middleware.go`; `apps/server/main.go` lines 172-178 wraps the mux as `SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(BodyCap(mux)))))`. The `WithUserID` / `UserID` helpers are in `apps/server/internal/http/errors.go` and the auth middleware writes through them via the `userIDSink` (errors.go lines 96-142).
**E2E test directory:** `tests/e2e/phase-1/access-log-fields-and-wiring/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | The access log line emitted by `AccessLog` includes `remote_ip=<ip>` (host portion of `r.RemoteAddr`, leftmost `X-Forwarded-For` when `CHAT_TRUSTED_PROXY=1`) and `user_id=<id>` (set by an authenticated handler via context helper; empty when unauthenticated). | missing | — |
| AC-2 | `apps/server/main.go` wraps the mux so that every request flows through `RequestIDMiddleware → AccessLog → Recover` (outermost to innermost) before reaching `wsapi.Handler`. | missing | — |
| AC-3 | `/ws` continues to upgrade successfully through the middleware stack (the existing `statusRecorder.Hijack` path is exercised by an integration test). | missing | — |
| AC-4 | A panic raised inside any handler under the wired stack is caught by `Recover`, never crashes the server process, and produces the user-safe envelope already implemented. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — access log records remote_ip and user_id**
- **What to assert:** Boot the server with stdout/stderr captured to a buffer (`cmd.Stdout = &buf`). Register and log in a user, then `GET /api/auth/me` with the bearer token. Scan the captured log lines for one matching `method=GET path=/api/auth/me status=200`; assert that line contains `remote_ip=127.0.0.1` and `user_id=<the registered user id from /api/auth/me response>`. Then make an anonymous `GET /debug/subs?channel=%23general`; assert its log line has `remote_ip=127.0.0.1` and either `user_id=` (empty) or no `user_id=` non-empty value.
- **Layer:** Go (boot server binary, capture stderr).
- **File path:** `tests/e2e/phase-1/access-log-fields-and-wiring/access_log_fields_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Replace `cmd.Stdout/Stderr = os.Stderr` with a `bytes.Buffer` guarded by a `sync.Mutex` so the test goroutine can scan lines as they appear.
- **Helpers it can reuse:** none — first test in the dir. Define `startServer(t)` (returning `runningServer` with the log buffer attached), `randomSecret`, `freePort`, `waitForPort` per the gold standard, plus `register(t,srv)` / `login(t,srv)` HTTP helpers and `awaitLogLine(t, srv, predicate, timeout)`.

**AC-2 — middleware chain wired in main.go**
- **What to assert:** Boot the binary; `GET /debug/subs?channel=%23general`. Assert the response carries an `X-Request-Id` header (any non-empty value), proving `RequestIDMiddleware` is in the chain. Then read the captured log buffer for a line referencing that same request id and the path `/debug/subs`, proving `AccessLog` is in the chain and sees the id (i.e. `RequestIDMiddleware` is outer to `AccessLog`). Finally, if the AC-4 panic probe is wired (skip otherwise), confirm the panic response also carries `X-Request-Id` plus the recovered envelope, proving outer→inner ordering of `AccessLog → Recover`.
- **Layer:** Go (boot binary).
- **File path:** `tests/e2e/phase-1/access-log-fields-and-wiring/chain_wiring_test.go`.
- **Setup it needs:** same as AC-1 (binary + log capture).
- **Helpers it can reuse:** `startServer(t)` from AC-1's harness file.

**AC-3 — /ws upgrade still succeeds through the middleware stack**
- **What to assert:** Boot the server, dial `ws://127.0.0.1:<port>/ws` with `coder/websocket.Dial` (no auth in default config — the gold-standard test does this), assert HTTP `101 Switching Protocols`. Then read the captured log buffer for a line with `path=/ws status=101` (or whatever status `AccessLog` records for a hijacked upgrade — verify by reading `middleware.go` once during test authoring) to prove the request flowed through the wrapped chain rather than bypassing it.
- **Layer:** Go (boot binary, dial WS).
- **File path:** `tests/e2e/phase-1/access-log-fields-and-wiring/ws_through_chain_test.go`.
- **Setup it needs:** same as AC-1; add `github.com/coder/websocket` for the dial.
- **Helpers it can reuse:** `startServer(t)` from harness.

**AC-4 — panic in handler caught by Recover, server stays up, envelope returned**
- **What to assert:** Recover only fires when a handler panics, and the production binary has no route that deliberately panics. Two options: (a) gate this test behind a build tag and ship a `tests/e2e/.../panicprobe` build tag that registers `GET /debug/panic` in the binary; (b) `t.Skip("requires panic probe build tag")` and document it. With the probe present, `GET /debug/panic` should return HTTP 500 with body `{"ok":false,"data":null,"error":{"code":"internal","message":"..."}}` (envelope from `errors.go`), and a follow-up `GET /debug/subs?channel=%23general` should still return 200, proving the server process survived.
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/access-log-fields-and-wiring/recover_panic_test.go`.
- **Setup it needs:** same as AC-1, plus the panic probe build tag (or `t.Skip`).
- **Helpers it can reuse:** `startServer(t)` from harness.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. This feature also needs the harness to capture stdout/stderr into a thread-safe buffer (extend `runningServer` with a `logs *syncBuf` field).

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/access-log-fields-and-wiring/harness_test.go` with copied helpers + `syncBuf` log capture + `register(t,srv)`/`login(t,srv)` HTTP helpers + `awaitLogLine(t, srv, substr, timeout)` scanner.
- Add `tests/e2e/phase-1/access-log-fields-and-wiring/access_log_fields_test.go` (AC-1), `chain_wiring_test.go` (AC-2), `ws_through_chain_test.go` (AC-3), `recover_panic_test.go` (AC-4 — `t.Skip` if no panic probe).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
