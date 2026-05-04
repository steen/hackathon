---
feature: auth-endpoints
phase: phase-1
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 7
covered: 0
partial: 0
missing: 7
deferred: 0
---

# E2E test analysis: Auth endpoints (register, login, me, logout, ws-ticket)

**Spec:** `specs/plans/phase-1/feature-auth-endpoints.md`
**Implementation status:** implemented — handlers in `apps/server/internal/http/auth_handlers.go`; ticket store in `apps/server/internal/auth/tickets.go`; JWT middleware in `apps/server/internal/auth/middleware.go`; route table in `apps/server/main.go` lines 133-137; `auth_events` writes via `repository`. `scripts/smoke.sh` exercises register → login → ws-ticket against the live binary.
**E2E test directory:** `tests/e2e/phase-1/auth-endpoints/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `POST /api/auth/register` requires a valid invite code (`CHAT_INVITE_CODE`) and creates a user with a hashed password (US-1, US-11). | missing | — |
| AC-2 | `POST /api/auth/login` returns a JWT including a `tv` claim on success; constant-time on failure (US-2). | missing | — |
| AC-3 | `GET /api/auth/me` returns the current user when given a valid bearer token; 401 otherwise. | missing | — |
| AC-4 | `POST /api/auth/logout` increments the user's `token_version`, invalidating all outstanding tokens (US-12). | missing | — |
| AC-5 | `POST /api/auth/ws-ticket` issues a one-shot, 30-second ticket bound to the user, redeemable once at WS upgrade (see `feature-ws-hardening.md`). | missing | — |
| AC-6 | All auth endpoints write entries to `auth_events`. | missing | — |
| AC-7 | `scripts/smoke.sh` continues to exit 0 after this feature lands. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — register requires invite code, persists hashed password**
- **What to assert:** Boot the server with random `CHAT_INVITE_CODE`. `POST /api/auth/register` body `{"username":"alice","password":"<random-12-byte-hex>","invite_code":"<correct>"}` → assert 200 (or 201) envelope `{ok:true, data:{user:{id,username:"alice"}}, error:null}`; the `id` is a 26-char ULID. Then with wrong invite_code → assert 4xx envelope `{ok:false, error:{code:"...", message:"..."}}`. Then with no invite_code field → assert 4xx envelope. Open the SQLite file at `CHAT_DB_PATH` read-only via `database/sql` (driver `modernc.org/sqlite`) and `SELECT username, password_hash FROM users WHERE username='alice'` → assert one row, assert `password_hash` starts with `$2a$` or `$2b$` (bcrypt prefix), assert `password_hash != "<random-12-byte-hex>"` (no plaintext).
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/auth-endpoints/register_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. After server starts, open the sqlite DB read-only via `database/sql`.
- **Helpers it can reuse:** none — first test in dir. Define `startServer(t)`, `randomSecret`, `freePort`, `waitForPort` per the gold standard, plus `register(t, srv, u, p)` and `openDBReadOnly(t, srv)` helpers.

**AC-2 — login returns JWT with `tv` claim, constant-time on failure**
- **What to assert:** Register a user, then `POST /api/auth/login` body `{"username":"alice","password":"<correct>"}` → 200 envelope `{ok:true, data:{token:"..."}}`. Decode the JWT (split on `.`, base64-url-decode the payload, json.Unmarshal); assert claims contain `sub` (matching the user ULID), `tv` (integer, value 0 right after register), `exp` (future). With wrong password → assert 401 envelope; record wall-clock latency. With unknown username → assert 401 envelope and *byte-identical* error `message` (compare strings) and `code` to the wrong-password case (PRD §9 SEC-4). Run unknown-vs-wrong-password 50 times each, assert `abs(median(unknown) - median(wrong))/median(wrong) < 0.30` as a sanity check (timing test, not strict — see auth-internals AC-3).
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/auth-endpoints/login_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness from AC-1; `decodeJWTPayload(t, tok) map[string]any`.

**AC-3 — `/api/auth/me` returns current user with valid bearer; 401 otherwise**
- **What to assert:** Register + login to obtain `token`. `GET /api/auth/me` with `Authorization: Bearer <token>` → 200 envelope `{ok:true, data:{id:"<ULID>", username:"alice"}}`. With no Authorization header → 401 envelope `{ok:false, error:{code:"unauthorized",...}}`. With `Authorization: Bearer <garbage>` → 401. With `Authorization: Bearer <valid-but-tampered-signature>` (flip last char of the signature segment) → 401. With expired token → 401 (skip if test cannot mint an expired one without secret access; the env-var route gives us the secret, so a small in-test JWT minter can produce one).
- **Layer:** Go (boot binary, HTTP).
- **File path:** `tests/e2e/phase-1/auth-endpoints/me_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness from AC-1; `register(t,...)`, `login(t,...)`, optional `mintJWT(t, secret, claims)` for the expired-token case.

**AC-4 — logout increments `token_version`, invalidates outstanding tokens**
- **What to assert:** Register + login user A, capture `tokenA1`. `GET /api/auth/me` with `tokenA1` → 200. `POST /api/auth/logout` with `tokenA1` → 200 envelope. `GET /api/auth/me` with the *same* `tokenA1` again → 401 (proves `tv` mismatch invalidates). Login again, capture `tokenA2`; assert `tokenA2 != tokenA1`. Decode both JWT payloads; assert `tv` claim of `tokenA2` is exactly `tv` of `tokenA1` + 1. Open SQLite read-only and `SELECT token_version FROM users WHERE username='alice'` → assert it equals the new `tv`.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/auth-endpoints/logout_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness from AC-1; `register`, `login`, `me`, `logout`, `decodeJWTPayload`, `openDBReadOnly`.

**AC-5 — ws-ticket is single-use, 30s TTL, bound to user, redeemable on WS**
- **What to assert:** Register + login. `POST /api/auth/ws-ticket` with bearer → 200 envelope `{ok:true, data:{ticket:"<non-empty>"}}`. Dial `ws://127.0.0.1:<port>/ws?ticket=<ticket>` with `coder/websocket.Dial` → 101. Dial again with the *same* ticket → not 101 (single-use; assert HTTP 401/403 from `Dial`'s returned `*http.Response`). Issue a fresh ticket; wait 31s; dial → not 101 (TTL expired). (Skip the 31s wait under `-short`.) Issue ticket from user A and from user B; assert tickets differ; assert dialing `/ws?ticket=<A>` succeeds but the resulting connection's bound user is A (assertable later via channel-scoped echo or `/debug/subs`).
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/auth-endpoints/ws_ticket_test.go`.
- **Setup it needs:** same as AC-1; `github.com/coder/websocket`.
- **Helpers it can reuse:** harness from AC-1; `wsTicket(t, srv, token)`; `dialWS(t, srv, ticket)`.

**AC-6 — every auth endpoint writes to `auth_events`**
- **What to assert:** Register + login + logout + ws-ticket all on user A; then a deliberate failure (login with wrong password) and a register with bad invite code. Open SQLite read-only; `SELECT kind, user_id FROM auth_events ORDER BY id`. Assert the rows include at minimum: one row with `kind='register'` and `user_id` = A's id; one with `kind='login'` (success) for A; one with `kind='logout'` for A; one with `kind='ws_ticket'` (or whatever `auth_handlers.go` actually writes — verify the kind strings by reading the handler once during test authoring) for A; one with `kind='login_failed'` (or equivalent) for A; one with `kind='register_failed'` (or equivalent) for the bad invite. Each row must carry a non-null `at` and (for failures) the IP `127.0.0.1`.
- **Layer:** Go (boot binary, HTTP, sqlite read).
- **File path:** `tests/e2e/phase-1/auth-endpoints/auth_events_test.go`.
- **Setup it needs:** same as AC-1.
- **Helpers it can reuse:** harness from AC-1; `openDBReadOnly`, `selectAuthEvents(db) []eventRow`.

**AC-7 — `scripts/smoke.sh` exits 0**
- **What to assert:** Run `bash scripts/smoke.sh` from the repo root via `exec.Command` with env `CHAT_SERVER_PORT=<freePort>`, `CHAT_JWT_SECRET=<random>`, `CHAT_INVITE_CODE=<random>`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`. Assert `cmd.Run()` returns nil (exit code 0). Capture combined output to attach to test failures via `t.Logf`.
- **Layer:** Go (drives bash script).
- **File path:** `tests/e2e/phase-1/auth-endpoints/smoke_script_test.go`.
- **Setup it needs:** bash on PATH; `repoRoot(t)`; free port; random secrets.
- **Helpers it can reuse:** `repoRoot(t)`, `freePort(t)`, `randomSecret(t,n)` from harness.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally. Extend the harness with: HTTP helpers (`register`, `login`, `me`, `logout`, `wsTicket`), a JWT-payload decoder, and a `database/sql` read-only opener that uses the `modernc.org/sqlite` driver already in `go.mod`.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/auth-endpoints/harness_test.go` with copied helpers + `register(t,srv,u,p)`, `login(t,srv,u,p)`, `me(t,srv,tok)`, `logout(t,srv,tok)`, `wsTicket(t,srv,tok)`, `openDBReadOnly(t,srv)`, `decodeJWTPayload(t,tok)`.
- Add one test file per logical area: `register_test.go`, `login_test.go`, `me_test.go`, `logout_test.go`, `ws_ticket_test.go`, `auth_events_test.go`, `smoke_script_test.go`.
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
