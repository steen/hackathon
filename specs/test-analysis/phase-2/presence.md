---
feature: presence
phase: phase-2
analyzed_at: 2026-05-04T01:40Z
analyzed_commit: 00b10ce9349fb1372c624e01d8c77bf0738747de
implementation_status: implemented
total_acs: 5
covered: 0
partial: 0
missing: 5
deferred: 0
---

# E2E test analysis: Presence (online users)

**Spec:** `specs/plans/phase-2/50-feature-presence.md`
**Implementation status:** implemented — `apps/server/internal/hub/hub.go` defines `AddPresence(userID) bool` and `RemovePresence(userID) bool` plus a per-user reference-count map; `apps/server/main.go:149-153` registers `GET /api/presence` (gated by `RequireJWT`) backed by `httpapi.NewPresenceHandlers`. Per-package presence test file exists at `apps/server/internal/hub/presence_test.go`.
**E2E test directory:** `tests/e2e/phase-2/presence/` (does not exist yet)

## Acceptance criteria

| AC | Statement | Status | E2E test reference |
|----|-----------|--------|---------------------|
| AC-1 | The server tracks the set of currently connected (authenticated) users derived from active WS connections. | missing | — |
| AC-2 | An event is broadcast when a user connects or disconnects (`presence` event with kind `join` / `leave`). | missing | — |
| AC-3 | A REST endpoint `GET /api/presence` returns the current online user IDs/usernames. | missing | — |
| AC-4 | The web app shows online users in the chat page; the CLI `chatd watch` optionally surfaces presence events. | missing | — |
| AC-5 | Presence is consistent if the same user has multiple connections (counted as online while at least one connection is open). | missing | — |

## Findings

### Missing E2E tests

These tests boot `apps/server` end-to-end (the same `startServer(t)` shape from `tests/server-ws-hub/hub_test.go`) and drive real HTTP + WS clients. The `presence_test.go` file in `apps/server/internal/hub/` is package-internal unit-level — it doesn't satisfy E2E coverage of the wired-up `/api/presence` route or the WS broadcast path that goes through `wsapi.Handler`.

Per-AC sketches:

- **AC-1 — `tests/e2e/phase-2/presence/tracking_test.go`.** Boot server. Register two users via REST (`alice`, `bob`); collect tokens. For each, mint a ticket via `GET /api/auth/ws-ticket` (Authorization: Bearer <token>), then dial `ws://127.0.0.1:<port>/ws?ticket=<hex>&channel=#general`. Wait for both upgrades to complete (poll `/debug/subs?channel=%23general` until count == 2). Then `GET /api/presence` (with alice's bearer) and assert the response body lists both `alice` and `bob`. The shape of the response is whatever `httpapi.NewPresenceHandlers.List` returns — read `apps/server/internal/http/presence_handlers.go` (referenced by `main.go:149-153`) before pinning the JSON shape.

- **AC-2 — `tests/e2e/phase-2/presence/events_test.go`.** Boot server. Register alice and bob. Alice connects WS first; collect inbound frames on a goroutine into a channel. Then bob connects WS (after alice is settled — poll `/debug/subs` to confirm). Within 2s, alice's inbound channel should yield a frame matching `{"type":"presence","kind":"join","user_id":"<bob-id>"}` (or `username` — pin by reading the impl). Then bob disconnects (`Close()`); within 2s, alice should see a `{"kind":"leave",...}` frame for bob. Use a `select` with a 2s timeout per expected event so the test fails fast on regressions.

- **AC-3 — `tests/e2e/phase-2/presence/endpoint_test.go`.** Three sub-tests:
  1. **Empty:** with zero WS connections, `GET /api/presence` returns 200 + empty list.
  2. **One user:** alice dials WS; assert the endpoint returns `[alice]`.
  3. **Auth required:** `GET /api/presence` without bearer returns 401 (the route is wrapped in `require` per `main.go:153`).

- **AC-4 — `tests/e2e/phase-2/presence/clients_surface_test.go`.** This AC spans two clients (web + CLI). Two halves:
  1. **CLI surfacing:** boot server + build chatd. Register alice + bob. Alice runs `chatd --server <url> watch #general` with stdout captured. Bob dials a WS connection (any client). Assert alice's chatd stdout contains a presence event line (format depends on impl — read `apps/cli/cmd/watch.go` to see whether presence is rendered as `[+] bob joined` or as raw JSON; the AC says "optionally surfaces" so the test should accept either-or-suppressed by reading the impl first; if the impl suppresses presence by default, this sub-test asserts a `--show-presence` flag (or similar) gates it).
  2. **Web surfacing:** covered by the `web-app` feature's AC-5 test (`tests/e2e/phase-2/web-app/presence.spec.ts`). This findings doc shouldn't duplicate it; cross-reference instead.

  If the chatd impl doesn't expose presence today (the spec uses "optionally"), mark the CLI sub-test as the only required half of AC-4 and assert the impl-defined behavior — silence is acceptable per "optionally", in which case the test asserts that no extraneous garbage appears on stdout when bob joins.

- **AC-5 — `tests/e2e/phase-2/presence/multi_connection_test.go`.** Boot server. Register alice. Open three WS connections from alice (three tickets, three dials). `GET /api/presence` returns alice exactly once (not three times). Close one connection; `GET /api/presence` still returns alice. Close another; still returns alice. Close the last; poll `GET /api/presence` until it no longer contains alice (or until a 2s timeout fails). Also assert: only one `kind:"join"` event was emitted (on the first connection) and only one `kind:"leave"` event (on the last disconnect). Wire the event assertion via a fourth WS subscriber (bob) that records all inbound presence frames; expect exactly one join and one leave for alice across the test.

### Helpers and harness notes

- Reuse the shared `tests/e2e/internal/serverharness/` package recommended in the other phase-2 findings — same boot env contract.
- For ticket-mint + WS-dial, write a `dialAuthenticatedWS(t, srv, token, channel) *websocket.Conn` helper that hits `/api/auth/ws-ticket` and then dials `?ticket=<hex>&channel=...`. Every presence test needs it; inlining is noisy.
- The `/debug/subs` endpoint is the right poll target to confirm a WS subscription has registered before doing the actual presence assertion. Don't `time.Sleep(100*time.Millisecond)` — poll with a 2s deadline.
- Connection counting (AC-5) is racy — close `c.Close()` on a websocket.Conn returns synchronously but the server-side cleanup runs on its read loop. Always poll `/api/presence` (or `/debug/subs`) until the expected state, with a deadline; never assert immediately after `Close()`.
- For AC-2, the test consumes WS frames as JSON — use `json.Unmarshal` into a typed envelope `{Type, Kind, UserID, Username string}` so an unrelated frame (e.g., a chat message) doesn't accidentally satisfy a substring assertion.

## Recommendations for /test-implement

1. **First:** `dialAuthenticatedWS` helper + `tests/e2e/phase-2/presence/endpoint_test.go` (AC-3) — smallest, validates server boot + the `/api/presence` route is wired.
2. **Then:** AC-1 (tracking) — extends the helper with two users.
3. **Then:** AC-2 (events) — needs a frame-collector helper but otherwise straightforward.
4. **Then:** AC-5 (multi-connection) — most assertions, biggest test.
5. **Last:** AC-4 (clients surface) — depends on chatd build; the web half lives under the `web-app` feature's E2E suite and should not be duplicated here.
