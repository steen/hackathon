---
feature: server-ws-hub
phase: phase-0
analyzed_at: 2026-05-03T14:06:50Z
analyzed_commit: 4902b5f55fc78b6ea268a001d0aec33d5ad34ff8
implementation_status: implemented
total_acs: 5
covered: 5
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Server WebSocket endpoint with in-memory hub

**Spec:** `specs/plans/phase-0/feature-server-ws-hub.md`
**Implementation status:** implemented — `apps/server/main.go` boots an `http.Server` on a configurable port, mounts `/ws` via `apps/server/internal/wsapi.Handler`, and the handler subscribes each conn to `#general` on the in-memory hub at `apps/server/internal/hub`.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `apps/server` exposes a `/ws` WebSocket endpoint. | covered | `apps/server/internal/wsapi/handler_test.go::TestHandlerBroadcastsBetweenClients` + `tests/server-ws-hub/hub_test.go::TestAC1_ServerWsHub_WsEndpointAccepts101Upgrade` |
| AC-2 | An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase. | covered | `apps/server/internal/hub/hub_test.go::TestBroadcastIsolatedPerChannel` + `tests/server-ws-hub/hub_test.go::TestAC2_ServerWsHub_HardcodedGeneralChannel` |
| AC-3 | Every received message is broadcast to all subscribers of the message's channel. | covered | `apps/server/internal/hub/hub_test.go::TestBroadcastReachesAllSubscribers` + `tests/server-ws-hub/hub_test.go::TestAC3_ServerWsHub_BroadcastReachesAllSubscribers` |
| AC-4 | Server starts via `go run ./apps/server` and listens on a configurable port (env var or default). | covered | `tests/server-ws-hub/hub_test.go::TestAC4_ServerWsHub_ServerListensOnConfiguredPort` (builds the binary, launches it with `CHAT_SERVER_PORT=<random>`, dials that port) |
| AC-5 | No authentication is required at this stage. | covered | `tests/server-ws-hub/hub_test.go::TestAC5_ServerWsHub_NoAuthorizationHeaderRequiredOnUpgrade` |

## Findings

### Covered

- **AC-1** — `wsapi.Handler` is exercised end-to-end via `httptest.NewServer`. The bootstrap-era skipped placeholder in `tests/server-ws-hub/hub_test.go` has been replaced with a live test that dials `/ws` against a fresh `httptest` server and asserts the upgrade succeeds.
- **AC-2** — `wsapi.Handler` constant `defaultChannel = "#general"` plus the per-channel isolation already tested in `apps/server/internal/hub/hub_test.go::TestBroadcastIsolatedPerChannel`. Anchored from `tests/` by a new `TestAC2_…HardcodedGeneralChannel` that subscribes via the handler and asserts both clients see the same broadcast (would fail if the handler subscribed to per-conn ephemeral channels).
- **AC-3** — Same plumbing as AC-2; new test in `tests/server-ws-hub/hub_test.go` dials three clients, sends one message from one of them, asserts all three receive it.
- **AC-5** — New test dials `/ws` with no `Authorization` header and asserts the upgrade succeeds. Complements the CLI-side `apps/cli/cmd/no_auth_test.go`.

### Architecture note

`apps/server/internal/{hub,wsapi}` are intentionally `internal/` and not importable from `tests/`. The system tests in `tests/server-ws-hub/hub_test.go` therefore go through the public boundary: they `go build ./apps/server` into a temp binary, launch it with `CHAT_SERVER_PORT=<random>` chosen via `net.Listen(":0")`, wait for the port, and dial via `github.com/coder/websocket`. Each test runs its own server instance (~700 ms each) — could be optimized to share a binary via `TestMain`, but the current shape is straightforward and the total cost is tolerable.

## Recommendations

1. Real (not skipped) AC tests now anchored in `tests/server-ws-hub/hub_test.go`; covers all 5 ACs by exercising the published binary + protocol surface.
2. Optional optimization: share the built binary across tests via `TestMain` to drop ~3 s of redundant `go build` cost.
3. Update spec `Files expected to be touched or created` to drop `apps/server/go.mod` (project uses single root go.mod per `CLAUDE.md`).
