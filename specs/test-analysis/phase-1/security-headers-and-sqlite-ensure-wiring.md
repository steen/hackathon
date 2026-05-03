---
feature: security-headers-and-sqlite-ensure-wiring
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

# E2E test analysis: Wire SecurityHeaders middleware and call EnsureFile at startup

**Spec:** `specs/plans/phase-1/feature-security-headers-and-sqlite-ensure-wiring.md`
**Implementation status:** implemented — `apps/server/main.go:174` builds `Handler: SecurityHeaders(RequestIDMiddleware(AccessLog(Recover(BodyCap(mux)))))` so `SecurityHeaders` is the outermost wrap. The DB open path at `main.go:79-96` calls `appdb.Open(dbPath)` which delegates to `apps/server/internal/db/perms.go` `EnsureFile` to enforce 0600.
**E2E test directory:** `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | Every response — including those written by `Recover` after a panic and those produced by the `/ws` upgrade path — carries the four SEC-10 headers (`Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options`). | missing | — |
| AC-2 | `SecurityHeaders` is layered as the outermost middleware so even error envelopes written by inner layers inherit the headers. | missing | — |
| AC-3 | `db.EnsureFile(path)` is invoked from `apps/server/main.go` at startup before any code opens the SQLite file (path comes from `CHAT_DB_PATH`, default `./chat.db`). | missing | — |
| AC-4 | A startup smoke test asserts that, after `main` boots against a fresh temp dir, the configured DB file exists with mode `0600`. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — four SEC-10 headers on every response, including panic and /ws**
- **What to assert:** Boot the server. Hit a representative success route (`GET /debug/subs?channel=%23general`) and a representative error route (`GET /api/auth/me` with no Authorization → 401). For each, assert the four headers are present with the documented values (pin from `headers_middleware.go`). Then dial `/ws` with `coder/websocket.Dial`; the returned `*http.Response` is the 101 upgrade response — assert the four headers on it. Finally, the panic arm: requires the panic-probe build tag (`t.Skip` if absent); after `GET /debug/panic` returns 500, assert all four headers on that response.
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/headers_everywhere_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Optional panic-probe build tag for the panic arm.
- **Helpers it can reuse:** none — first test in dir. Define harness per gold standard plus `requireSecHeaders(t, h http.Header)`.

**AC-2 — `SecurityHeaders` is outermost (errors from inner middleware still carry headers)**
- **What to assert:** Concrete proof of "outermost": trigger responses that come from each inner layer and verify headers are present.
  - From `BodyCap` (innermost concrete error): `POST /api/auth/register` with a 16385-byte body → 413; assert four headers.
  - From `Recover` (panic probe, `t.Skip` if absent): `GET /debug/panic` → 500; assert four headers.
  - From `AccessLog` itself does not write responses, but observe via `X-Request-Id` (set by `RequestIDMiddleware`) — assert that even on the 413 response from `BodyCap`, `X-Request-Id` is present, proving `RequestIDMiddleware` is outer to `BodyCap` (transitive ordering check).
  - From the mux (404): `GET /nope` → 404; assert four headers.
  All four cases sharing the four headers proves `SecurityHeaders` wraps the entire chain. (Negative shape: if the order were reversed and `SecurityHeaders` wrapped only the mux, the panic-recovered response would lack the headers — that's exactly what this AC guards against.)
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/headers_outermost_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `requireSecHeaders`.

**AC-3 — `EnsureFile` called before the SQLite file is opened**
- **What to assert:** The visible signal of "EnsureFile ran" is the file existing with mode 0600 after boot. Boot the binary against `CHAT_DB_PATH=<tmpdir>/chatd.sqlite` (file does not exist beforehand). Wait for the port to listen. Stat the file → assert it exists, mode is 0600 (unix). Repeat with the file pre-existing in mode 0644 → after boot, mode is 0600 (proves `EnsureFile` is invoked even when the file pre-exists, not only on first creation). Confirm via the `/api/auth/register` round-trip that the SQLite open actually succeeded (a row reaches the `users` table) — proves `EnsureFile` ran *before* `db.Open` succeeded, not after.
- **Layer:** Go (boot binary, file stat + HTTP).
- **File path:** `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/ensure_file_called_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness; `register`.

**AC-4 — startup smoke: file exists with 0600 after main boots**
- **What to assert:** Pretty much the same as AC-3's first half but framed as a single tight smoke. Boot against fresh `CHAT_DB_PATH`; wait for port; stat the file; assert `info.Mode().Perm() == 0o600` (skip on Windows). The deliberate duplication keeps the AC numbering 1:1 with the spec; the test is short.
- **Layer:** Go (boot binary, file stat).
- **File path:** `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/startup_file_mode_smoke_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. Pin the `Content-Security-Policy` value by reading `headers_middleware.go` once during test authoring; if the CSP is intentionally test-mutable, instead assert non-empty.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/security-headers-and-sqlite-ensure-wiring/harness_test.go` with copied helpers + `requireSecHeaders(t, h http.Header)`, `register(t, srv, u, p)`.
- Add `headers_everywhere_test.go` (AC-1), `headers_outermost_test.go` (AC-2), `ensure_file_called_test.go` (AC-3), `startup_file_mode_smoke_test.go` (AC-4 — skip on Windows).
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
