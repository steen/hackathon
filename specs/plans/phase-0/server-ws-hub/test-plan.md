# Test plan: Server WebSocket endpoint with in-memory hub

**Feature plan:** [feature-server-ws-hub.md](../feature-server-ws-hub.md)
**Parent phase:** [Phase 0: Walking skeleton, system-test ready](../../phase-0-walking-skeleton-system-test-ready.md)
**PRD revision:** 7e33be3

## Note on requirement IDs

The feature plan provided no `requirement_ids` — only acceptance criteria. Per the test-plan rules, "a requirement with no tests is a bug — flag it"; the inverse problem applies here (acceptance criteria with no PRD requirement ID to anchor against). To keep tests grep-able, this plan introduces local IDs `AC-1`…`AC-5` that map 1:1 to the acceptance criteria below. If/when the PRD assigns formal IDs to these criteria, rename accordingly.

| Local ID | Acceptance criterion |
|----------|----------------------|
| AC-1 | `apps/server` exposes a `/ws` WebSocket endpoint. |
| AC-2 | An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase. |
| AC-3 | Every received message is broadcast to all subscribers of the message's channel. |
| AC-4 | Server starts via `go run ./apps/server` and listens on a configurable port (env var or default). |
| AC-5 | No authentication is required at this stage. |

## Coverage matrix

| Requirement ID | Description | Unit tests | E2E tests |
|----------------|-------------|------------|-----------|
| AC-1 | `/ws` endpoint exposed | 1 | 1 |
| AC-2 | Per-channel subscriber tracking, hardcoded `#general` | 3 | 1 |
| AC-3 | Broadcast to all subscribers of a channel | 2 | 1 |
| AC-4 | Configurable port via env var with default | 2 | 1 |
| AC-5 | No auth required | 1 | 1 |

## Unit tests

### AC-1 — `/ws` endpoint exposed

- **Name:** `TestAC1_WSHandler_UpgradesHTTPToWebSocket`
  - **Target file:** `apps/server/internal/server/ws_handler_test.go`
  - **Asserts:**
    - GET `/ws` with valid `Upgrade: websocket` headers returns HTTP 101 Switching Protocols
    - non-WebSocket GET to `/ws` returns a 4xx (e.g. 400) without panicking
    - response includes `Sec-WebSocket-Accept` header

### AC-2 — Per-channel subscriber tracking, hardcoded `#general`

- **Name:** `TestAC2_Hub_SubscribeRegistersSubscriberOnChannel`
  - **Target file:** `apps/server/internal/hub/hub_test.go`
  - **Asserts:**
    - after `Subscribe(ch, sub)`, the hub reports the subscriber as a member of `ch`
    - subscribing the same subscriber twice on the same channel does not duplicate delivery on later broadcast

- **Name:** `TestAC2_Hub_UnsubscribeRemovesSubscriber`
  - **Target file:** `apps/server/internal/hub/hub_test.go`
  - **Asserts:**
    - after `Unsubscribe(ch, sub)`, a subsequent `Broadcast(ch, msg)` does not deliver to that subscriber
    - unsubscribing an unknown subscriber is a no-op (no panic, no error)

- **Name:** `TestAC2_WSHandler_RegistersConnectionToGeneralChannel`
  - **Target file:** `apps/server/internal/server/ws_handler_test.go`
  - **Asserts:**
    - opening a `/ws` connection causes the hub to gain exactly one subscriber on channel `#general`
    - closing the connection removes that subscriber from `#general` (deferred unsubscribe runs)

### AC-3 — Broadcast to all subscribers of a channel

- **Name:** `TestAC3_Hub_BroadcastDeliversToAllSubscribersOfChannel`
  - **Target file:** `apps/server/internal/hub/hub_test.go`
  - **Asserts:**
    - with N subscribers on channel `ch`, `Broadcast(ch, msg)` causes each subscriber to receive `msg` exactly once
    - subscribers on a different channel do not receive `msg`

- **Name:** `TestAC3_Hub_BroadcastDoesNotBlockOnSlowSubscriber`
  - **Target file:** `apps/server/internal/hub/hub_test.go`
  - **Asserts:**
    - a subscriber with a full send buffer does not block delivery to other subscribers
    - the slow subscriber's outcome (drop or disconnect) is the documented behaviour, not an indefinite hang
  - **Note:** required to validate the "buffered send channel for writes" implementation step and the goroutine-leak risk.

### AC-4 — Configurable port via env var with default

- **Name:** `TestAC4_Config_PortFromEnvVar`
  - **Target file:** `apps/server/internal/config/config_test.go`
  - **Asserts:**
    - when the port env var is set to a valid value (e.g. `8081`), the resolved listen address uses that port
    - when set to an invalid value (non-numeric, out of range), config returns an error rather than silently falling back

- **Name:** `TestAC4_Config_DefaultPortWhenEnvUnset`
  - **Target file:** `apps/server/internal/config/config_test.go`
  - **Asserts:**
    - with the env var unset, the resolved port equals the documented default

### AC-5 — No authentication required

- **Name:** `TestAC5_WSHandler_AcceptsConnectionWithoutCredentials`
  - **Target file:** `apps/server/internal/server/ws_handler_test.go`
  - **Asserts:**
    - a `/ws` upgrade request with no `Authorization` header, no cookies, and no auth query params completes the upgrade (HTTP 101)
    - the connection is registered with the hub on `#general` regardless of caller identity

## E2E tests

E2E here means end-to-end against a real `go run ./apps/server` process (or an `httptest.Server` wrapping the real handler) using a real WebSocket client. No mocks of the hub or upgrader.

### AC-1 — `/ws` endpoint exposed

- **Name:** `TestE2E_AC1_ServerExposesWSEndpoint`
  - **Target file:** `apps/server/e2e/ws_e2e_test.go`
  - **Scenario:** start the server, dial `ws://<addr>/ws`, expect a successful handshake.
  - **Asserts:** dial returns no error and the connection is open.

### AC-2 — Per-channel subscriber tracking, hardcoded `#general`

- **Name:** `TestE2E_AC2_DisconnectRemovesSubscriberFromGeneral`
  - **Target file:** `apps/server/e2e/ws_e2e_test.go`
  - **Scenario:** open client A and client B; close A; have B send a message; A's underlying goroutine must have exited (no leak) and the hub's `#general` subscriber count must drop to 1.
  - **Asserts:**
    - after A closes, the hub reports exactly one `#general` subscriber (exposed via test-only accessor or metric)
    - no goroutine leak attributable to A's connection (use `goleak` or equivalent)

### AC-3 — Broadcast to all subscribers of a channel

- **Name:** `TestE2E_AC3_MessageFromOneClientReachesAllOthers`
  - **Target file:** `apps/server/e2e/ws_e2e_test.go`
  - **Scenario:** connect three clients to `/ws`, send a text frame from client 1, expect clients 2 and 3 to receive the same payload within a bounded timeout. Mirrors the manual `websocat` check from the feature plan's skeleton.
  - **Asserts:**
    - clients 2 and 3 each receive the exact payload exactly once
    - client 1's own delivery behaviour matches the documented choice (echo or no-echo) consistently

### AC-4 — Configurable port via env var with default

- **Name:** `TestE2E_AC4_ServerListensOnConfiguredPort`
  - **Target file:** `apps/server/e2e/ws_e2e_test.go`
  - **Scenario:** start the server with the port env var set to a free port chosen by the test; dial that port.
  - **Asserts:**
    - dial succeeds on the configured port
    - dial to the default port fails (port is not occupied by this server instance)

### AC-5 — No authentication required

- **Name:** `TestE2E_AC5_UnauthenticatedClientCanSendAndReceive`
  - **Target file:** `apps/server/e2e/ws_e2e_test.go`
  - **Scenario:** connect two clients with no auth headers/cookies; send from one; the other receives.
  - **Asserts:** broadcast succeeds end-to-end without any credential being supplied.

## Coverage rules

- Every local requirement ID (AC-1 … AC-5) has at least one unit test AND at least one E2E test.
- Test names start with the requirement ID for grep-ability.
- Tests describe behaviour from the acceptance criterion, not implementation details (no asserting on unexported hub internals beyond what is needed to verify subscribe/unsubscribe membership).
- If formal PRD requirement IDs are later assigned to these acceptance criteria, rename `AC-N` → the assigned ID in test names and this document.
