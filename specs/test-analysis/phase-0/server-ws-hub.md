---
feature: server-ws-hub
phase: phase-0
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 6
covered: 0
partial: 0
missing: 6
deferred: 0
---

# E2E test analysis: Server WebSocket endpoint with in-memory hub

**Spec:** `specs/plans/phase-0/feature-server-ws-hub.md`
**Implementation status:** implemented — `apps/server/internal/hub/hub.go` provides Subscribe/Unsubscribe/Broadcast; `apps/server/internal/wsapi/handler.go` upgrades `/ws` and `apps/server/internal/wsapi/debug_handler.go` serves `/debug/subs`.
**E2E test directory:** `tests/e2e/phase-0/server-ws-hub/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | `apps/server` exposes a `/ws` WebSocket endpoint. | missing | — |
| AC-2 | An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase. | missing | — |
| AC-3 | Every received message is broadcast to all subscribers of the message's channel. | missing | — |
| AC-4 | Server starts via `go run ./apps/server` and listens on a configurable port (env var or default). | missing | — |
| AC-5 | No authentication is required at this stage. | missing | — |
| AC-6 | `GET /debug/subs?channel=<name>` returns the current subscriber count for the given channel as plain text (decimal integer + `\n`). Internal-only (the `/debug/` prefix marks it as not part of the product API and not on the `{ok,data,error}` envelope contract); intended for CI scripts and tests to avoid sleep-based readiness races. | missing | — |

## Findings

### Missing E2E tests

- **AC-1 — `/ws` endpoint accepts WebSocket upgrade.**
  - **What to assert:** A `websocket.Dial` against `ws://127.0.0.1:<port>/ws` returns HTTP 101 Switching Protocols.
  - **Layer:** Go (boot server binary).
  - **File path:** `tests/e2e/phase-0/server-ws-hub/upgrade_test.go`
  - **Setup it needs:** Build `apps/server` into `t.TempDir()`, free port via `net.Listen("tcp","127.0.0.1:0")`, random `CHAT_JWT_SECRET` (32 bytes hex) and `CHAT_INVITE_CODE` (8 bytes hex) via `crypto/rand`, exec server with `CHAT_SERVER_PORT=<port>`.
  - **Helpers it can reuse:** no helper yet — first test for this feature will need to define `startServer(t)` modeled on `tests/server-ws-hub/hub_test.go`.

- **AC-2 — Hub tracks subscribers per channel; default `#general`.**
  - **What to assert:** Two unauthenticated WS dials both register against `#general`. Poll `GET /debug/subs?channel=%23general` until it returns `2`. The pattern in `tests/server-ws-hub/hub_test.go::TestAC2_*` (debug-subs based) is the right shape.
  - **Layer:** Go (boot server binary).
  - **File path:** `tests/e2e/phase-0/server-ws-hub/hub_subscribers_test.go`
  - **Setup it needs:** Same as AC-1 plus an `http.Get` client for the `/debug/subs` probe.
  - **Helpers it can reuse:** the local `startServer(t)` and a `fetchSubscriberCount(url)` parser (also copied from the gold-standard harness).

- **AC-3 — Every received message is broadcast to all subscribers.**
  - **What to assert:** Audit #78 reframed this AC: inbound WS frames are now silently dropped to prevent peer impersonation. The honest E2E for "broadcast reaches all subscribers" must drive the producer path that still exists (REST `POST /api/channels/{id}/messages`) and assert that two WS subscribers both observe a `{type:"message",...}` envelope. Negative twin: a frame written by client A must NOT echo to client B (mirrors `TestAC3_ServerWsHub_InboundFramesDropped` in the existing `tests/server-ws-hub/hub_test.go`).
  - **Layer:** Go (boot server binary).
  - **File path:** `tests/e2e/phase-0/server-ws-hub/broadcast_test.go`
  - **Setup it needs:** Same as AC-1. The REST producer path needs an authenticated user — model the register+login dance on `apps/cli/cmd/register.go` + `login.go`. The negative inbound-drop assertion needs no auth.
  - **Helpers it can reuse:** `startServer(t)`; if a register+login helper is needed, write it locally.

- **AC-4 — Server listens on configured port (env var).**
  - **What to assert:** Setting `CHAT_SERVER_PORT=<port>` causes the server to listen on exactly that port. `waitForPort(<port>, 10s)` succeeds; a dial against that port upgrades.
  - **Layer:** Go (boot server binary).
  - **File path:** `tests/e2e/phase-0/server-ws-hub/port_test.go`
  - **Setup it needs:** Free port + env var; same harness.
  - **Helpers it can reuse:** `startServer(t)` already drives this path; the test is a belt-and-suspenders dial.

- **AC-5 — No `Authorization` header required on `/ws` upgrade.**
  - **What to assert:** `websocket.Dial` with no `Authorization` header returns 101. Verify against the current `apps/server/internal/wsapi/handler.go` first — if the handler now requires auth despite the spec wording, the test should still be written and the AC flipped to `deferred` because the impl that satisfied it was removed.
  - **Layer:** Go (boot server binary).
  - **File path:** `tests/e2e/phase-0/server-ws-hub/no_auth_required_test.go`
  - **Setup it needs:** Same harness; nil `websocket.DialOptions`.
  - **Helpers it can reuse:** `startServer(t)`.

- **AC-6 — `/debug/subs?channel=<name>` returns subscriber count.**
  - **What to assert:** `GET /debug/subs?channel=%23general` returns `Content-Type: text/plain` and a body matching `^\d+\n$`. With zero dials open the count is 0; after one dial the count rises to 1; after the dial closes the count returns to 0 (poll within a deadline). Optional stronger check: PR #87 loopback-gates the endpoint, so a non-loopback request would be rejected — exercising that needs a non-loopback bind, which is environment-dependent; a single positive-path 127.0.0.1 GET is the minimum.
  - **Layer:** Go (boot server binary).
  - **File path:** `tests/e2e/phase-0/server-ws-hub/debug_subs_test.go`
  - **Setup it needs:** Same harness; `http.Get` against the `/debug/subs` URL.
  - **Helpers it can reuse:** `startServer(t)`, `fetchSubscriberCount(url)`.

### Partial / suspect coverage

(None — `tests/e2e/` does not exist yet.)

### Helpers and harness notes

`tests/server-ws-hub/hub_test.go` is the gold-standard pattern for booting `apps/server` in a Go test: it builds the binary in `t.TempDir()`, picks a free port via `net.Listen("tcp", "127.0.0.1:0")`, generates a random `CHAT_JWT_SECRET` and `CHAT_INVITE_CODE` via `crypto/rand`. The first E2E test for any feature should copy `startServer(t)`, `randomSecret(t, n)`, `freePort(t)`, `waitForPort(...)`, and the `runningServer` struct verbatim into a sibling `harness_test.go` in the per-feature dir. Do not import them — that test's package is `server_ws_hub_test`, the helpers are intentionally local.

## Recommendations for /test-implement

- Create `tests/e2e/phase-0/server-ws-hub/harness_test.go` with `startServer(t)`, `randomSecret`, `freePort`, `waitForPort`, `repoRoot`, `fetchSubscriberCount`, and the `runningServer` struct copied verbatim from `tests/server-ws-hub/hub_test.go`.
- Add `tests/e2e/phase-0/server-ws-hub/upgrade_test.go` with `TestAC1_ServerWsHub_UpgradeReturns101`.
- Add `tests/e2e/phase-0/server-ws-hub/hub_subscribers_test.go` with `TestAC2_ServerWsHub_SubscribersOnGeneral`.
- Add `tests/e2e/phase-0/server-ws-hub/broadcast_test.go` with `TestAC3_ServerWsHub_BroadcastReachesAllSubscribers` (positive via REST producer) and a negative `TestAC3_ServerWsHub_InboundFramesDropped`.
- Add `tests/e2e/phase-0/server-ws-hub/port_test.go` with `TestAC4_ServerWsHub_ListensOnConfiguredPort`.
- Add `tests/e2e/phase-0/server-ws-hub/no_auth_required_test.go` with `TestAC5_ServerWsHub_NoAuthHeaderRequired` — verify against current `handler.go` before asserting; flip to deferred if the impl no longer matches.
- Add `tests/e2e/phase-0/server-ws-hub/debug_subs_test.go` with `TestAC6_ServerWsHub_DebugSubsCount`.
