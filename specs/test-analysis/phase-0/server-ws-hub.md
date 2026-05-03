---
feature: server-ws-hub
phase: phase-0
analyzed_at: 2026-05-03T20:38:00Z
analyzed_commit: a283ba3df1d16750dfe0ccbd8e4f370dd6519c68
implementation_status: implemented
total_acs: 6
covered: 6
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Server WebSocket endpoint with in-memory hub

**Spec:** `specs/plans/phase-0/feature-server-ws-hub.md`
**Implementation status:** implemented — `apps/server/main.go` boots an `http.Server` on a configurable port, mounts `/ws` via `apps/server/internal/wsapi.Handler`, and the handler subscribes each conn to `#general` on the in-memory hub at `apps/server/internal/hub`. **Audit #78 (PR #85) re-framed AC-3** to drop the raw inbound rebroadcast — the spec wording is unchanged but its load-bearing reading flipped from "WS receive → broadcast" to "REST receive → broadcast; WS inbound silently dropped". Tests at `tests/server-ws-hub/hub_test.go` were re-flipped accordingly. AC-6 (`/debug/subs`) was loopback-gated by PR #87 — internal-only contract was always there in the spec wording, but the gate makes it enforced rather than implicit.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `apps/server` exposes a `/ws` WebSocket endpoint. | covered | `apps/server/internal/wsapi/handler_test.go::TestHandlerDoesNotRebroadcastInboundFrames` (post-#78 replacement for the old `TestHandlerBroadcastsBetweenClients`) + `tests/server-ws-hub/hub_test.go::TestAC1_ServerWsHub_WsEndpointAccepts101Upgrade` |
| AC-2 | An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase. | covered | `apps/server/internal/hub/hub_test.go::TestBroadcastIsolatedPerChannel` + `tests/server-ws-hub/hub_test.go::TestAC2_ServerWsHub_HardcodedGeneralChannel` (post-#78: now verifies shared-channel registration via the `/debug/subs?channel=#general` count rather than the old "client A writes, client B reads" probe). |
| AC-3 | Every received message is broadcast to all subscribers of the message's channel. | covered (re-framed by audit #78) | **Spec text unchanged; load-bearing reading flipped.** The supported producer is now `POST /api/channels/{id}/messages` (which calls `hub.Broadcast` after persisting); raw WS inbound frames are silently dropped. Tests:<br>(a) The hub primitive itself: `apps/server/internal/hub/hub_test.go::TestBroadcastReachesAllSubscribers` (unchanged — Broadcast still fans out).<br>(b) The new negative assertion: `tests/server-ws-hub/hub_test.go::TestAC3_ServerWsHub_InboundFramesDropped` (post-#78: peer writes a frame, asserts other subscribers do NOT receive it within the deadline — timeout is the success path).<br>(c) The supported broadcast path is exercised by `apps/server/internal/http/ws_broadcast_test.go::TestWSSubscriberReceivesBroadcastMessage` (left unchanged — it drives REST POST → WS subscriber receives). |
| AC-4 | Server starts via `go run ./apps/server` and listens on a configurable port (env var or default). | covered | `tests/server-ws-hub/hub_test.go::TestAC4_ServerWsHub_ServerListensOnConfiguredPort` (builds the binary, launches it with `CHAT_SERVER_PORT=<random>`, dials that port) |
| AC-5 | No authentication is required at this stage. | covered | `tests/server-ws-hub/hub_test.go::TestAC5_ServerWsHub_NoAuthorizationHeaderRequiredOnUpgrade` |
| AC-6 | `GET /debug/subs?channel=<name>` returns the current subscriber count as plain text (decimal + `\n`). Internal-only; not on the `{ok,data,error}` envelope. | covered | `apps/server/internal/wsapi/debug_handler_test.go::TestDebugSubsHandler` (5 subtests: 2-sub channel, 1-sub channel, unknown-channel zero, missing-param 400, non-GET 405) + new `debug_handler_test.go` cases from PR #87 covering the loopback gate (rejects requests from non-127.0.0.1 / non-::1 sources with 403). The smoke script's runtime poll continues to exercise the endpoint. |

## Findings

### Audit #78 reframing — AC-3 contract change

The spec text "Every received message is broadcast to all subscribers of the message's channel" was always ambiguous between two readings:

1. **Original (phase-0) reading:** WS receive → broadcast. A client writes a frame on its WS conn; the server fans it out to every conn on the same channel. This was the load-bearing interpretation through phase-0; `chatd send`/`chatd watch` rely on it.
2. **Post-#78 reading:** REST receive → broadcast. A client POSTs to `/api/channels/{id}/messages`; the server persists a `messages` row, attributes `sender_user_id` from the JWT, and *then* broadcasts the typed `{type:"message",data:<Message>}` envelope via `hub.Broadcast` to every subscriber.

The audit finding was that reading (1) lets any authenticated peer forge `{type, data}` envelopes carrying arbitrary `sender_user_id` and impersonate other users to every subscriber on the channel — no DB write, no audit row. The fix in commit `92d447f` drops the `h.Broadcast(channel, data)` call from `wsapi.handler.go::readLoop`. The read still happens (drains the buffer; enforces `MessageBodyLimit` + per-conn token bucket), but the bytes are discarded.

The test at `apps/server/internal/wsapi/handler_test.go::TestHandlerDoesNotRebroadcastInboundFrames` is the negative anchor. The phase-0 system test `TestAC3_ServerWsHub_InboundFramesDropped` is its system-level mirror.

**Spec follow-up (out of test-agent scope):** rewrite AC-3 to either say "broadcast on REST receive" explicitly or reference the new contract documented in `feature-channels-and-messages` AC-4. The current ambiguous phrasing will keep tripping future readers.

### AC-6 loopback gate (PR #87)

The spec calls `/debug/subs` "internal-only" but didn't pin a network-level enforcement. PR #87 added a loopback check in `apps/server/internal/wsapi/debug_handler.go` — non-loopback sources get 403. This is consistent with the spec wording ("internal-only") and makes the contract enforced rather than relying on operators to firewall the port. The `apps/server/main.go` change adds the loopback-trust config (single line). Smoke script's runtime poll runs from 127.0.0.1 so it's unaffected.

### Architecture note (unchanged from prior tick)

`apps/server/internal/{hub,wsapi}` are intentionally `internal/` and not importable from `tests/`. The system tests in `tests/server-ws-hub/hub_test.go` therefore go through the public boundary: they `go build ./apps/server` into a temp binary, launch it with `CHAT_SERVER_PORT=<random>` chosen via `net.Listen(":0")`, wait for the port, and dial via `github.com/coder/websocket`. Each test runs its own server instance (~700 ms each).

### Cross-feature impact of #78

- **`feature-cli-send-watch` AC-1 silently regressed.** `chatd send` (`apps/cli/cmd/send.go`) still writes a raw text frame on the WS conn. Its in-package test (`TestAC_0_1_SendWritesSingleTextFrameToWebSocket`) still passes because it only asserts the frame is *written*, not that anything observed it server-side. Post-#78, the server drops the frame — `chatd send` is functionally a no-op. See `cli-send-watch.md` AC-1 partial flag.
- **`feature-channels-and-messages` AC-4 is now the sole producer path.** Already covered, but its load-bearing-ness increased — every message in the system now flows through `POST /api/channels/{id}/messages`.
- **Smoke script rewired** (commits `cf82cdc`, `142e8b7`): now uses REST POST + per-channel watchers via `chatd watch --ticket=...` rather than the old `chatd send` → broadcast → `chatd watch` flow.

## Recommendations

1. No new tests added by this run — the existing tests were correctly re-flipped by the audit #78 fix author. The negative-assertion (`TestAC3_ServerWsHub_InboundFramesDropped`) and positive-assertion (`TestWSSubscriberReceivesBroadcastMessage` in the http package) jointly anchor the new contract.
2. **Spec follow-up (out of test-agent scope):** rewrite AC-3 to remove the ambiguity between "WS-receive → broadcast" and "REST-receive → broadcast". Reference `feature-channels-and-messages` AC-4 explicitly.
3. **Cross-feature follow-up:** `cli-send-watch` AC-1 should re-evaluate to `partial` — `chatd send` now emits a frame the server drops. Either deprecate the `send` command or rewrite it to POST against `/api/channels/{id}/messages` (which would require auth wiring on the CLI; that's `20-feature-cli-full-commands`'s job).
