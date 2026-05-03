---
feature: channels-and-messages
phase: phase-1
analyzed_at: 2026-05-03T15:59:48Z
analyzed_commit: 000a530af83bf62890252da5df553b153eff33ae
implementation_status: partial
total_acs: 6
covered: 1
partial: 5
missing: 0
deferred: 0
---

# Test analysis: Channels and messages endpoints (REST + WS)

**Spec:** `specs/plans/phase-1/feature-channels-and-messages.md`
**Implementation status:** partial — handlers + repo accessors ship and pass strong unit/integration tests against `httptest.NewServer`; WS handler now reads `?channel=` and subscribes to it; messages-create broadcasts the persisted record to the hub. **But `apps/server/main.go` does NOT mount `/api/channels` or `/api/channels/{id}/messages`.** A live `GET /api/channels` against the running binary returns 404 because the routes are unregistered. Same wiring-gap pattern as `feature-file-perms-and-headers` (PR #37).

## Acceptance criteria

| AC | Statement (verbatim from spec) | Status | Test reference |
|----|-------------------------------|--------|----------------|
| AC-1 | `GET /api/channels` returns the list of channels (US-3). | partial | `channels_handlers_test.go::TestListChannelsReturnsCreatedChannels` covers handler behavior end-to-end via httptest. **Wiring gap:** main.go has no `mux.Handle("/api/channels", ...)`. A live request returns 404. |
| AC-2 | `POST /api/channels` with `{name}` creates a channel and returns it (US-4); rejects duplicate or invalid names. | partial | `channels_handlers_test.go::TestCreateChannelPersistsAndReturnsIt` + `TestCreateChannelRejectsDuplicateName` + `TestCreateChannelRejectsInvalidName` (regex: 1-40 chars lowercase/digits/hyphens, must start with letter/digit). Repo-layer reinforces with `repo/channels_test.go::TestCreateChannelRejectsDuplicateName`. **Wiring gap:** same — route not on the live mux. |
| AC-3 | `GET /api/channels/{id}/messages?before=&limit=` returns prior messages, newest-first, paginated (US-6). | partial | `messages_handlers_test.go::TestGetMessagesReturnsPriorMessagesPaginated` + `TestGetMessagesUnknownChannelReturns404`. Repo-layer: `repo/messages_test.go::TestListMessagesReturnsNewestFirstAndPaginates` + `TestListMessagesCapsLimit` (cap at `MaxMessagesLimit`). **Wiring gap:** same. |
| AC-4 | `POST /api/channels/{id}/messages` persists a message and broadcasts it to WS subscribers of that channel (US-5). | partial | `messages_handlers_test.go::TestPostMessagePersistsAndBroadcasts` (handler-level: insert-then-broadcast ordering verified) + `TestPostMessageRejectsUnknownChannel` + `TestPostMessageRejectsBadBodies`. Repo-layer: `repo/messages_test.go::TestInsertMessagePersistsRow`. **Wiring gap:** same. |
| AC-5 | WS clients receive new-message events in real time, with author + timestamp + body (US-5). | partial | `ws_broadcast_test.go::TestWSSubscriberReceivesBroadcastMessage` is end-to-end: drives `/api/channels/{id}/messages` POST through one `httptest.Server`, dials `/ws?channel=<id>` against another, asserts the subscriber receives the persisted record envelope. wsapi.Handler now reads `?channel=` (capped at 64 chars to bound LRU memory). **Wiring gap:** same — the messages route exists in test setup but not in main.go, so a live binary cannot reproduce this flow without the route being mounted. The WS half (`/ws?channel=ID`) is correctly wired in main.go. |
| AC-6 | All endpoints require authentication (bearer token via REST, ticket-redeemed JWT for WS). | covered | `channels_handlers_test.go::TestChannelsEndpointsRequireAuth` exercises every channels/messages REST route through `auth.RequireJWT` and asserts 401 without a bearer. The handler `Routes()` helper composes the middleware in the test setup; if main.go ever wires the routes, it must use the same `Routes()` helper to inherit the auth wrap (the WS-side ticket redemption belongs to the not-yet-shipped `feature-ws-hardening`). |

## Findings

### Partial — wiring gap for AC-1 through AC-5

`grep -nE 'mux.Handle|/api/channels' apps/server/main.go` shows the live mux registers `/api/{register,login,me,logout,ws-ticket}`, `/ws`, `/debug/subs`. **No `/api/channels` route.** A `curl -X GET http://localhost:PORT/api/channels` against the running binary at this SHA returns 404.

The handlers expose a `Routes(...)` method that takes the auth middleware and registers both `/api/channels` and `/api/channels/{id}/messages` (Go 1.22+ `ServeMux` placeholders). main.go just doesn't call it. The fix is roughly:

```go
ch := httpapi.NewChannelsHandlers(httpapi.ChannelsDeps{Repo: repository})
ms := httpapi.NewMessagesHandlers(httpapi.MessagesDeps{Repo: repository, Hub: h})
ch.Routes(mux, require)
ms.Routes(mux, require)
```

— a few lines, same shape as the existing `ah.Register / Login / …` registrations. The handler unit tests use `httptest.NewServer` with a mux they construct themselves, so they exercise the same code path the live mux would; the gap is purely in main.go's wiring step. AC-6 stays `covered` because the handler-level auth-required test does drive the same `RequireJWT` middleware that main.go would use.

**No failing system test added by this run.** Same call as PR #37 (file-perms-and-headers wiring gap): a guaranteed-red `tests/channels-and-messages/` test would put `pnpm test` in a permanent-fail state until the maintainer's wiring PR closes it. The findings doc + this PR body make the gap unambiguous; the next PR closes it. The optional cheap anchor would be a static `grep` test asserting `mux.Handle("/api/channels"` exists in main.go — explicit but lightweight.

### AC-5 broadcast-channel ID mismatch is a real concern worth flagging

The `messages` handler broadcasts via `h.Broadcast(channelID, payload)` where `channelID` is the ULID from the URL path. The WS handler subscribes via `?channel=<id>`. Both sides must agree on the channel-key shape. `ws_broadcast_test.go` exercises this end-to-end with a real ULID, so the agreement is verified — but the legacy phase-0 default (`?channel=` absent → `defaultChannel = "#general"`) is now a separate channel from any ULID-keyed one. **A WS client without `?channel=` joins `#general` and will not see broadcasts from the new messages endpoint** unless someone separately broadcasts to `#general`. That's by design at this SHA (the existing phase-0 raw-rebroadcast on `#general` still works for the smoke script), but it'll surprise a web/CLI client that expects "open WS, get channel events". Worth a spec follow-up to clarify.

### AC-6 design observation — Routes() composition

The handler's `Routes(mux, mw)` accepts the auth middleware as a parameter rather than baking it in. Right call: it lets tests pass a no-op middleware to exercise the handler logic without forging JWTs, while main.go passes the real `require := auth.RequireJWT(...)`. The handler-level test `TestChannelsEndpointsRequireAuth` constructs the live `RequireJWT` middleware to prove the auth-wrap works — that's what makes AC-6 covered, not partial.

### Cross-feature observation — closes the partial flag on `feature-sqlite-schema-and-ulid` AC-4

That AC was previously marked partial in PR #40: schema permits ULIDs (`TEXT PRIMARY KEY`), `ids.NewULID()` exists, but no shipped INSERT path used it. **It's now closed.** `apps/server/internal/http/channels_handlers.go:75` (`id := ids.NewULID()`) and `messages_handlers.go:136` (same) are the load-bearing INSERT call sites. The data-layer contract "all PK inserts use NewULID" is now real. The next test-watch tick after this PR merges should re-promote `feature-sqlite-schema-and-ulid` AC-4 from `partial` to `covered`.

(There's an interesting subtlety: `messages_handlers.go:108` references "the canonical uppercase form `ids.NewULID().String()` emits" in a comment, which means the handler is also normalizing user-supplied ULID input. The repo's GetChannel rejects non-canonical-case ULIDs, which is what the comment is alerting future readers to. Not load-bearing for any AC; just good factoring.)

### Spec-vs-impl notes

- Spec lists `apps/server/internal/hub/hub.go (update for channel scoping)` as expected. The hub itself was already channel-keyed (`Subscribe(channel, sub)` shipped in phase-0); this PR only adjusted the WS handler to thread the channel through the upgrade. Functionally equivalent.
- Spec doesn't mandate the channel-name regex shape; impl picks `^[a-z0-9][a-z0-9-]{0,39}$` (lowercase, digits, hyphens, must start with alphanumeric). Documented in the handler's error message. Reasonable; spec follow-up could pin it explicitly.
- Pagination cap at `MaxMessagesLimit` (200 per spec); enforced by `repo/messages_test.go::TestListMessagesCapsLimit` via the `repo.DefaultMessagesLimit` constant.

## Recommendations

1. **Production wiring:** add `ch.Routes(mux, require); ms.Routes(mux, require)` to `apps/server/main.go` — a few lines. This promotes AC-1 through AC-5 from partial to covered. Same shape as the existing auth-handler registrations.
2. **No new tests added** — the handler/repo coverage at the unit + integration boundary is comprehensive. Adding a failing system test for the wiring gap would put `pnpm test` in a permanent-fail state until the wiring PR ships; the findings doc + PR body are the clearer signal.
3. **Cross-feature follow-up:** when this PR merges, re-evaluate `phase-1/sqlite-schema-and-ulid` AC-4 — it's now load-bearing-real, not just permitted-by-schema. Should re-promote from partial to covered.
4. **Spec follow-up (out of test-agent scope):** clarify the WS subscription model for clients that don't pass `?channel=`. Today they join `#general` and miss broadcasts from the messages endpoint. A future spec should either pin "WS without channel param is an error" or "WS without channel param subscribes to all channels the user is in".
