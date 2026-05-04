---
feature: auth-endpoint-paths-align-with-prd
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

# E2E test analysis: Align auth endpoint paths with PRD §10

**Spec:** `specs/plans/phase-1/feature-auth-endpoint-paths-align-with-prd.md`
**Implementation status:** implemented — `apps/server/main.go` lines 133-137 register the five handlers under the `/api/auth/...` prefix exactly as PRD §10 mandates (`/api/auth/register`, `/api/auth/login`, `/api/auth/me`, `/api/auth/logout`, `/api/auth/ws-ticket`). Behaviour comes from `apps/server/internal/http/auth_handlers.go`.
**E2E test directory:** `tests/e2e/phase-1/auth-endpoint-paths-align-with-prd/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | The server registers handlers at `/api/auth/register`, `/api/auth/login`, `/api/auth/me`, `/api/auth/logout`, `/api/auth/ws-ticket` (matching PRD §10). | missing | — |
| AC-2 | Existing tests (`apps/server/internal/http/auth_handlers_test.go`) and `scripts/smoke.sh` are updated to use the new paths and continue to pass. | missing | — |
| AC-3 | No other behavioural change — request/response shapes, JWT semantics, ticket TTL, audit-log entries all remain as PR #38 implemented them. This plan is path-only. | missing | — |
| AC-4 | Either the parent plan (`feature-auth-endpoints.md`) is amended to reflect `/api/auth/*` paths, or it is left alone with a one-line note that the PRD wins. Either is acceptable; not aligning the parent plan would be misleading. | missing | — |

## Findings

### Missing E2E tests

**AC-1 — five handlers live at the PRD §10 paths**
- **What to assert:** Boot the binary. For each of the five paths, verify the route is mounted by sending an unauthenticated request that the handler is known to reject *with its own envelope* (not with the default ServeMux 404). For `POST /api/auth/register` and `POST /api/auth/login` send `{}` and assert response status is one of {400, 401, 422} with body `{"ok":false,"error":{"code":"...","message":"..."}}` (envelope from `errors.go`) — never `404 not_found`. For `GET /api/auth/me`, `POST /api/auth/logout`, `POST /api/auth/ws-ticket` send no `Authorization` header and assert 401 with `{"ok":false,"error":{"code":"unauthorized",...}}`. Also assert `POST /api/register`, `/api/login`, etc. (no `/auth/`) return 404, proving the old paths are not silently aliased.
- **Layer:** Go (boot binary).
- **File path:** `tests/e2e/phase-1/auth-endpoint-paths-align-with-prd/paths_test.go`.
- **Setup it needs:** built `chat-server` binary in `t.TempDir()`, free port, `CHAT_JWT_SECRET=randomSecret(t,32)`, `CHAT_INVITE_CODE=randomSecret(t,8)`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`.
- **Helpers it can reuse:** none — first test in dir. Define `startServer(t)`, `randomSecret`, `freePort`, `waitForPort`, `runningServer` per gold standard, plus a `requireEnvelope(t, body, ok bool, code string)` JSON-decode helper.

**AC-2 — smoke.sh paths align**
- **What to assert:** This AC is about the in-tree smoke script and the in-package Go test using the new paths. The E2E suite only has a black-box vantage; the realistic E2E proxy is "the smoke script exits 0 against a fresh binary." Run `bash scripts/smoke.sh` from the repo root with `CHAT_SERVER_PORT=<freePort>`, `CHAT_JWT_SECRET=<random>`, `CHAT_INVITE_CODE=<random>`, `CHAT_DB_PATH=<tmpdir>/chatd.sqlite`; assert `cmd.Run()` returns nil. As a complementary signal, also `grep -F '/api/register' scripts/smoke.sh` from the test (read the file via `os.ReadFile`) and assert the old path string is absent — proves the script was updated, not just left to drift.
- **Layer:** Go (drives bash script + reads file).
- **File path:** `tests/e2e/phase-1/auth-endpoint-paths-align-with-prd/smoke_script_test.go`.
- **Setup it needs:** same env as AC-1; bash on PATH; `repoRoot(t)` helper.
- **Helpers it can reuse:** `repoRoot(t)`, `freePort(t)`, `randomSecret(t,n)` from harness.

**AC-3 — request/response shapes unchanged (path-only refactor)**
- **What to assert:** End-to-end golden flow at the new paths: register → login → me → ws-ticket → redeem ws-ticket on `/ws` → logout → me-after-logout. Assert at each step: register returns 200/201 envelope with `data.user.{id,username}` (id is a 26-char ULID); login returns 200 envelope with `data.token` (non-empty JWT-shaped string); me returns 200 envelope with `data.{id,username}` matching register; ws-ticket returns 200 envelope with `data.ticket` (non-empty string); WS upgrade with `?ticket=<value>` returns 101; logout returns 200; me with the same bearer after logout returns 401 (proves `tv` increment still works).
- **Layer:** Go (boot binary, HTTP + WS).
- **File path:** `tests/e2e/phase-1/auth-endpoint-paths-align-with-prd/golden_flow_test.go`.
- **Setup it needs:** same as AC-1; `github.com/coder/websocket`.
- **Helpers it can reuse:** harness from AC-1; `register(t,srv)`, `login(t,srv,user,pass)`, `me(t,srv,token)`, `wsTicket(t,srv,token)` HTTP helpers.

**AC-4 — parent plan or PRD-wins note present in repo**
- **What to assert:** This is documentation hygiene, not runtime behaviour. The E2E proxy: read `specs/plans/phase-1/feature-auth-endpoints.md` from disk and assert either (a) every route reference uses `/api/auth/<verb>`, or (b) the file contains the literal phrase `PRD §10` paired with a note acknowledging the path delta. Skip if the spec file is absent.
- **Layer:** Go (file read).
- **File path:** `tests/e2e/phase-1/auth-endpoint-paths-align-with-prd/spec_consistency_test.go`.
- **Setup it needs:** `repoRoot(t)` helper; no server boot.
- **Helpers it can reuse:** `repoRoot(t)` from harness.

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern. The first test in this feature dir should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and `runningServer` into a sibling `harness_test.go`. Do not import them across packages — copy locally.

## Recommendations for /test-implement

- Create `tests/e2e/phase-1/auth-endpoint-paths-align-with-prd/harness_test.go` with copied helpers + `register(t,srv)` / `login(t,srv,user,pass)` / `me(t,srv,token)` / `wsTicket(t,srv,token)` HTTP helpers and `requireEnvelope(t,body,...)`.
- Add one test file per AC: `paths_test.go`, `smoke_script_test.go`, `golden_flow_test.go`, `spec_consistency_test.go`.
- Each test name: `TestACN_<CamelCase>` with the literal `AC-N` token also in a leading comment.
