# Feature: Presence (online users)

**Parent phase:** [Phase 2: Web UI + shared clients](../phase-2-web-ui-shared-clients.md)
**Status:** planned

## Requirements covered
- US-7 — As a user, I want to see who is currently online, so I know whether it's worth pinging now.

## Acceptance criteria
- The server tracks the set of currently connected (authenticated) users derived from active WS connections.
- An event is broadcast when a user connects or disconnects (`presence` event with kind `join` / `leave`).
- A REST endpoint `GET /api/presence` returns the current online user IDs/usernames.
- The web app shows online users in the chat page; the CLI `chatd watch` optionally surfaces presence events.
- Presence is consistent if the same user has multiple connections (counted as online while at least one connection is open).

## Implementation steps
1. Extend the in-memory hub to maintain a `userID → connectionCount` map, updated on connect/disconnect.
2. On the first connection for a user, broadcast `{type:"presence", kind:"join", user_id}`; on the last disconnect, broadcast `kind:"leave"`.
3. Add `GET /api/presence` returning the current online set.
4. Update `packages/api-client` and `packages/go-client` to expose `Presence()` and `presence` events on the WS stream.
5. Update `apps/web` to render an online-users list driven by the initial `GET /api/presence` plus presence events.

## Test plan
- `test_presence_endpoint_lists_online_users` — covers US-7.
- `test_presence_join_event_broadcast_on_connect` — covers US-7.
- `test_presence_leave_event_broadcast_on_last_disconnect` — covers US-7.
- `test_user_with_two_connections_appears_once` — covers US-7 multi-connection semantics.

## Files expected to be touched or created
- `apps/server/internal/hub/hub.go` (presence tracking)
- `apps/server/internal/http/presence_handlers.go`
- `apps/server/internal/http/presence_handlers_test.go`
- `packages/go-client/presence.go`
- `packages/api-client/src/presence.ts`
- `apps/web/src/components/OnlineUsers.tsx`

## Risks
- Counting connections rather than sessions can show users as online during transient reconnects; acceptable for the PRD's threat model and aligns with simplest correct semantics.
