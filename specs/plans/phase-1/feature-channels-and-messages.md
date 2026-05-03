# Feature: Channels and messages endpoints (REST + WS)

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** done (PR pending)

## Requirements covered
- US-3 — As a user, I want to see the list of channels, so I can pick where to talk.
- US-4 — As a user, I want to create a channel, so we can split topics.
- US-5 — As a user, I want to send a message into a channel and have every connected client see it in real time, so chat feels live.
- US-6 — As a user, I want to see prior messages when I open a channel, so I can catch up.

## Acceptance criteria
- `GET /api/channels` returns the list of channels (US-3).
- `POST /api/channels` with `{name}` creates a channel and returns it (US-4); rejects duplicate or invalid names.
- `GET /api/channels/{id}/messages?before=&limit=` returns prior messages, newest-first, paginated (US-6).
- `POST /api/channels/{id}/messages` persists a message and broadcasts it to WS subscribers of that channel (US-5).
- WS clients receive new-message events in real time, with author + timestamp + body (US-5).
- All endpoints require authentication (bearer token via REST, ticket-redeemed JWT for WS).

## Implementation steps
1. Add `apps/server/internal/repo/channels.go` and `messages.go` with `ListChannels`, `CreateChannel`, `ListMessages`, `InsertMessage`.
2. Add HTTP handlers in `apps/server/internal/http/channels_handlers.go` and `messages_handlers.go`.
3. Update the in-memory hub (from Phase 0) to be channel-aware: subscribers join by channel ID, broadcasts target a single channel.
4. On `POST /api/channels/{id}/messages`: insert into DB, then broadcast the persisted record to the hub.
5. Validate channel-name shape (e.g., lowercase, hyphenated, length cap) and reject duplicates.
6. Pagination: `before` is a ULID cursor; default `limit` is 50, max 200.

## Test plan
- `test_list_channels_returns_seeded_channels` — covers US-3.
- `test_create_channel_persists_and_returns_it` — covers US-4.
- `test_create_channel_rejects_duplicate_name` — covers US-4.
- `test_post_message_persists_and_broadcasts` — covers US-5.
- `test_ws_subscriber_receives_broadcast_message` — covers US-5.
- `test_get_messages_returns_prior_messages_paginated` — covers US-6.
- `test_endpoints_require_authentication` — covers cross-cutting auth.

## Files expected to be touched or created
- `apps/server/internal/repo/channels.go`
- `apps/server/internal/repo/messages.go`
- `apps/server/internal/repo/*_test.go`
- `apps/server/internal/http/channels_handlers.go`
- `apps/server/internal/http/messages_handlers.go`
- `apps/server/internal/http/*_test.go`
- `apps/server/internal/hub/hub.go` (update for channel scoping)

## Risks
- Race between persistence and broadcast could deliver out-of-order events; mitigated by inserting first, then broadcasting the persisted record (with its assigned ULID and timestamp).
