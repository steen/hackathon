---
feature: channels-and-messages
phase: phase-1
analyzed_at: 2026-05-03T17:26:50Z
analyzed_commit: fa60bfdd928918ed6813ff04b1c947e66dd78758
implementation_status: implemented
total_acs: 6
covered: 6
partial: 0
missing: 0
deferred: 0
---

# Test analysis: Channels and messages endpoints (REST + WS)

**Spec:** `specs/plans/phase-1/feature-channels-and-messages.md`
**Implementation status:** implemented — handlers + repo accessors ship; WS handler reads `?channel=` and subscribes to it; messages-create broadcasts the persisted record to the hub. The previous wiring gap (PR #55) is closed: `apps/server/main.go:125-133` constructs `ChannelsHandlers` + `MessagesHandlers` and calls `ch.Routes(mux, require, msg)` so `/api/channels` and `/api/channels/{id}/messages` are now mounted on the live mux behind `auth.RequireJWT`. All five previously-partial ACs re-promoted to covered at this SHA.

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `GET /api/channels` returns the list of channels (US-3). | covered | `channels_handlers_test.go::TestListChannelsReturnsCreatedChannels` covers handler behavior end-to-end via httptest. Live wiring confirmed: `apps/server/main.go:133` registers the route through `ch.Routes(mux, require, msg)`. |
| AC-2 | `POST /api/channels` with `{name}` creates a channel and returns it (US-4); rejects duplicate or invalid names. | covered | `channels_handlers_test.go::TestCreateChannelPersistsAndReturnsIt` + `TestCreateChannelRejectsDuplicateName` + `TestCreateChannelRejectsInvalidName` (regex: 1-40 chars lowercase/digits/hyphens, must start with letter/digit). Repo-layer reinforces with `repo/channels_test.go::TestCreateChannelRejectsDuplicateName`. Wired via `Routes()` at main.go:133. |
| AC-3 | `GET /api/channels/{id}/messages?before=&limit=` returns prior messages, newest-first, paginated (US-6). | covered | `messages_handlers_test.go::TestGetMessagesReturnsPriorMessagesPaginated` + `TestGetMessagesUnknownChannelReturns404`. Repo-layer: `repo/messages_test.go::TestListMessagesReturnsNewestFirstAndPaginates` + `TestListMessagesCapsLimit` (cap at `MaxMessagesLimit`). Wired via `Routes()` at main.go:133. |
| AC-4 | `POST /api/channels/{id}/messages` persists a message and broadcasts it to WS subscribers of that channel (US-5). | covered | `messages_handlers_test.go::TestPostMessagePersistsAndBroadcasts` (handler-level: insert-then-broadcast ordering verified) + `TestPostMessageRejectsUnknownChannel` + `TestPostMessageRejectsBadBodies`. Repo-layer: `repo/messages_test.go::TestInsertMessagePersistsRow`. Wired via `Routes()` at main.go:133. |
| AC-5 | WS clients receive new-message events in real time, with author + timestamp + body (US-5). | covered | `ws_broadcast_test.go::TestWSSubscriberReceivesBroadcastMessage` is end-to-end: drives `/api/channels/{id}/messages` POST through one `httptest.Server`, dials `/ws?channel=<id>` against another, asserts the subscriber receives the persisted record envelope. wsapi.Handler reads `?channel=` (capped at 64 chars). Both routes (WS + messages POST) are now on the live mux at this SHA. |
| AC-6 | All endpoints require authentication (bearer token via REST, ticket-redeemed JWT for WS). | covered | `channels_handlers_test.go::TestChannelsEndpointsRequireAuth` exercises every channels/messages REST route through `auth.RequireJWT` and asserts 401 without a bearer. main.go passes the real `require` middleware into `ch.Routes(mux, require, msg)`, so the live wiring inherits the same auth wrap as the test setup. WS-side ticket redemption is covered by `feature-ws-hardening` (PR #50) and `feature-ws-userid-binding-and-channel-existence-check` (gap-D). |

## Findings

### Wiring gap closed

`apps/server/main.go:125-133` (verified via grep at `fa60bfd`):

```
ch := httpapi.NewChannelsHandlers(httpapi.ChannelsDeps{...})
msg := httpapi.NewMessagesHandlers(httpapi.MessagesDeps{...})
ch.Routes(mux, require, msg)
```

The single-call `ch.Routes(mux, require, msg)` registers both `/api/channels` and `/api/channels/{id}/messages` (Go 1.22+ ServeMux placeholders) behind the live `auth.RequireJWT` middleware. A live `curl` against the running binary now reaches the handlers; the handler-level unit tests that were previously the only coverage are now backed by real route registration.

### AC-5 broadcast-channel ID mismatch — design note still applies

The `messages` handler broadcasts via `h.Broadcast(channelID, payload)` where `channelID` is the ULID from the URL path. The WS handler subscribes via `?channel=<id>`. Both sides agree on the channel-key shape, and `ws_broadcast_test.go` exercises this end-to-end with a real ULID. The legacy phase-0 default (`?channel=` absent → `defaultChannel = "#general"`) is a separate channel from any ULID-keyed one — a WS client without `?channel=` joins `#general` and will not see broadcasts from the new messages endpoint unless someone separately broadcasts to `#general`. By design at this SHA (the existing phase-0 raw-rebroadcast on `#general` still works for the smoke script), but a web/CLI client expecting "open WS, get channel events" needs to know to pass `?channel=`. Worth a spec follow-up.

### AC-6 design observation — Routes() composition

The handler's `Routes(mux, mw, msg)` accepts the auth middleware as a parameter rather than baking it in. Right call: it lets tests pass a no-op middleware to exercise the handler logic without forging JWTs, while main.go passes the real `require := auth.RequireJWT(...)`. The handler-level test `TestChannelsEndpointsRequireAuth` constructs the live `RequireJWT` middleware to prove the auth-wrap works.

### Cross-feature observation — closes the partial flag on `feature-sqlite-schema-and-ulid` AC-4

That AC was marked partial in PR #40: schema permits ULIDs (`TEXT PRIMARY KEY`), `ids.NewULID()` exists, but no shipped INSERT path used it. **Now closed and reachable via live mux.** `apps/server/internal/http/channels_handlers.go:77` (`id := ids.NewULID()`) and `messages_handlers.go:138` (same) are the load-bearing INSERT call sites. With main.go wiring those handlers to the live mux, the full chain "client request → handler → ULID-keyed INSERT" is exercised at runtime, not just in handler unit tests. The sister findings doc re-promotes AC-4 from partial to covered at this SHA.

### Spec-vs-impl notes

- Spec lists `apps/server/internal/hub/hub.go (update for channel scoping)` as expected. The hub itself was already channel-keyed (`Subscribe(channel, sub)` shipped in phase-0); this PR only adjusted the WS handler to thread the channel through the upgrade. Functionally equivalent.
- Spec doesn't mandate the channel-name regex shape; impl picks `^[a-z0-9][a-z0-9-]{0,39}$` (lowercase, digits, hyphens, must start with alphanumeric). Documented in the handler's error message. Reasonable; spec follow-up could pin it explicitly.
- Pagination cap at `MaxMessagesLimit` (200 per spec); enforced by `repo/messages_test.go::TestListMessagesCapsLimit` via the `repo.DefaultMessagesLimit` constant.

## Recommendations

1. No new tests added by this run — coverage at the unit + integration boundary plus the now-live route registration is comprehensive across all 6 ACs.
2. **Spec follow-up (out of test-agent scope):** clarify the WS subscription model for clients that don't pass `?channel=`. Today they join `#general` and miss broadcasts from the messages endpoint. A future spec should either pin "WS without channel param is an error" or "WS without channel param subscribes to all channels the user is in".
3. **Cross-feature confirmation:** the `feature-sqlite-schema-and-ulid` AC-4 partial flag re-promotes to covered at this SHA — see `sqlite-schema-and-ulid.md`.
