---
feature: server-ws-hub
phase: phase-0
analyzed_at: 2026-05-03T14:36:54Z
analyzed_commit: 979995f4036835473476118401f425f04de70106
implementation_status: implemented
total_acs: 6
covered: 6
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Server WebSocket endpoint with in-memory hub

**Spec:** `specs/plans/phase-0/feature-server-ws-hub.md`
**Implementation status:** implemented — `apps/server/main.go` boots an `http.Server` on a configurable port, mounts `/ws` via `apps/server/internal/wsapi.Handler`, and the handler subscribes each conn to `#general` on the in-memory hub at `apps/server/internal/hub`. PR #25 added a 6th AC (`/debug/subs`) and the corresponding handler.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `apps/server` exposes a `/ws` WebSocket endpoint. | covered | `apps/server/internal/wsapi/handler_test.go::TestHandlerBroadcastsBetweenClients` + `tests/server-ws-hub/hub_test.go::TestAC1_ServerWsHub_WsEndpointAccepts101Upgrade` |
| AC-2 | An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase. | covered | `apps/server/internal/hub/hub_test.go::TestBroadcastIsolatedPerChannel` + `tests/server-ws-hub/hub_test.go::TestAC2_ServerWsHub_HardcodedGeneralChannel` |
| AC-3 | Every received message is broadcast to all subscribers of the message's channel. | covered | `apps/server/internal/hub/hub_test.go::TestBroadcastReachesAllSubscribers` + `tests/server-ws-hub/hub_test.go::TestAC3_ServerWsHub_BroadcastReachesAllSubscribers` |
| AC-4 | Server starts via `go run ./apps/server` and listens on a configurable port (env var or default). | covered | `tests/server-ws-hub/hub_test.go::TestAC4_ServerWsHub_ServerListensOnConfiguredPort` (builds the binary, launches it with `CHAT_SERVER_PORT=<random>`, dials that port) |
| AC-5 | No authentication is required at this stage. | covered | `tests/server-ws-hub/hub_test.go::TestAC5_ServerWsHub_NoAuthorizationHeaderRequiredOnUpgrade` |
| AC-6 | `GET /debug/subs?channel=<name>` returns the current subscriber count as plain text (decimal + `\n`). Internal-only; not on the `{ok,data,error}` envelope. | covered | `apps/server/internal/wsapi/debug_handler_test.go::TestDebugSubsHandler` (5 subtests: 2-sub channel, 1-sub channel, unknown-channel zero, missing-param 400, non-GET 405) + indirect runtime exercise via `scripts/smoke.sh` (polls the endpoint until count==2 before publishing). |

## Findings

### Covered

- **AC-1** — `wsapi.Handler` is exercised end-to-end via `httptest.NewServer`. The bootstrap-era skipped placeholder in `tests/server-ws-hub/hub_test.go` has been replaced with a live test that dials `/ws` against a fresh `httptest` server and asserts the upgrade succeeds.
- **AC-2** — `wsapi.Handler` constant `defaultChannel = "#general"` plus the per-channel isolation already tested in `apps/server/internal/hub/hub_test.go::TestBroadcastIsolatedPerChannel`. Anchored from `tests/` by a new `TestAC2_…HardcodedGeneralChannel` that subscribes via the handler and asserts both clients see the same broadcast (would fail if the handler subscribed to per-conn ephemeral channels).
- **AC-3** — Same plumbing as AC-2; new test in `tests/server-ws-hub/hub_test.go` dials three clients, sends one message from one of them, asserts all three receive it.
- **AC-5** — New test dials `/ws` with no `Authorization` header and asserts the upgrade succeeds. Complements the CLI-side `apps/cli/cmd/no_auth_test.go`.
- **AC-6 (new in PR #25)** — `apps/server/internal/wsapi/debug_handler_test.go` covers the contract: GET-only (POST → 405), required `channel` param (missing → 400), text/plain content-type, decimal-int + newline body, zero for unknown channels. The smoke script's runtime poll (`scripts/smoke.sh:114-125`) is what makes the endpoint load-bearing — without it the script's two watchers race the publish.

### Architecture note

`apps/server/internal/{hub,wsapi}` are intentionally `internal/` and not importable from `tests/`. The system tests in `tests/server-ws-hub/hub_test.go` therefore go through the public boundary: they `go build ./apps/server` into a temp binary, launch it with `CHAT_SERVER_PORT=<random>` chosen via `net.Listen(":0")`, wait for the port, and dial via `github.com/coder/websocket`. Each test runs its own server instance (~700 ms each) — could be optimized to share a binary via `TestMain`, but the current shape is straightforward and the total cost is tolerable.

## Recommendations

1. Real (not skipped) AC tests now anchored in `tests/server-ws-hub/hub_test.go`; covers ACs 1–5. AC-6 is fully covered in-package; no `tests/`-layer anchor needed since the smoke script is the system-level user of the endpoint.
2. Optional optimization: share the built binary across tests via `TestMain` to drop ~3 s of redundant `go build` cost.
3. Update spec `Files expected to be touched or created` to add `apps/server/internal/wsapi/debug_handler.go` + `_test.go` and drop `apps/server/go.mod` (project uses single root go.mod per `CLAUDE.md`).
