# Feature: Server WebSocket endpoint with in-memory hub

**Parent phase:** [Phase 0: Walking skeleton, system test ready](../phase-0-walking-skeleton-system-test-ready.md)
**Status:** done (PR pending)

## Requirements covered
- (no user-story IDs from the PRD map directly; this is the wire-end groundwork for US-5 which lands fully in Phase 1)

## Acceptance criteria
- `apps/server` exposes a `/ws` WebSocket endpoint.
- An in-memory hub tracks subscribers per channel; channel is hardcoded to `#general` for this phase.
- Every received message is broadcast to all subscribers of the message's channel.
- Server starts via `go run ./apps/server` and listens on a configurable port (env var or default).
- No authentication is required at this stage.

## Implementation steps
1. Create `apps/server/main.go` with a minimal HTTP server.
2. Add a hub package (`apps/server/internal/hub/hub.go` or similar) with `Subscribe`, `Unsubscribe`, and `Broadcast` operations keyed by channel name.
3. Implement the `/ws` handler: upgrade the HTTP request, register the connection with the hub against `#general`, then read incoming text frames and broadcast them.
4. Use a goroutine per connection for reads and a buffered send channel for writes.
5. Ensure clean disconnect removes the subscriber from the hub.

## Test plan
- Unit test: hub broadcasts a message to all current subscribers and not to unsubscribed clients.
- Unit test: hub unsubscribe removes the subscriber and a subsequent broadcast does not reach it.
- Manual: connect with two `websocat` (or equivalent) clients to `/ws`, send from one, observe the other receive it.

## Files expected to be touched or created
- `apps/server/main.go`
- `apps/server/internal/hub/hub.go`
- `apps/server/internal/hub/hub_test.go`
- `apps/server/go.mod`

## Risks
- Goroutine leaks if disconnect handling is incomplete; mitigated by deferring unsubscribe in the handler.
