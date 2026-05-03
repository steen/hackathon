# Feature: Bind userID onto WS connection state + validate channel existence

**Parent phase:** [Phase 1: Persistence + auth](../phase-1-persistence-auth.md)
**Status:** planned

## Why this exists

Follow-up to [feature-ws-hardening](./feature-ws-hardening.md) (PR #39, status flipped to `done`). Two acceptance criteria from the parent plan are explicitly deferred in the merged code:

| Parent AC | Code reality | Source |
|---|---|---|
| "After successful ticket redemption, the WS connection is associated with the authenticated user identity." | `userID` is recovered from the redeemed ticket but discarded immediately: `_ = userID` with a TODO comment | `apps/server/internal/wsapi/handler.go:148-153` |
| "WS sends to non-existent channels are rejected with a typed error frame" | The handler subscribes to whatever string is in `?channel=` (capped at 64 chars). No DB lookup; no `{type:"error", code:"CHANNEL_NOT_FOUND"}` frame is ever produced. The plan's "Implementation notes" call this out as a coordination point pending PR #42. | `apps/server/internal/wsapi/handler.go:122-132` (subscribe) and `:165-180` (readLoop broadcasts raw `data` without frame parsing) |

PR #42 (channels-and-messages) has now landed, so the dependency the parent plan was waiting for is in place. This plan ties off both items.

## Requirements covered

- US-5 (real-time send/receive) — server-side guarantee that messages are tagged with the right sender and that subscribers are joined to channels that actually exist.
- PRD §9 (auth flow) — bound identity is what `messages.user_id` writes need to attribute the sender once messages are sent through the WS path.
- PRD §10 lines 402-414 — the WS protocol distinguishes `subscribe`, `unsubscribe`, `send` frames (this plan does not yet implement that envelope; see "Out of scope" below).
- PRD §14 risk row "WS abuse: oversize frames, flooding, channel-ID spoofing" — channel-existence check closes the spoofing arm.

## Acceptance criteria

- A redeemed ticket's `userID` is stored on a per-connection state value (e.g., a `connState` struct) and reachable from `readLoop` so subsequent message-handling code can attribute the sender.
- On WS upgrade with a `?channel=<id>` query parameter, the handler validates the channel exists via the same `repo.ListChannels`/`repo.GetChannel`-style lookup the REST handlers use. Unknown channel IDs reject the upgrade with HTTP 404 and the standard error envelope.
- For the legacy `#general` and the seeded ULID channels, validation passes.
- The TODO at `apps/server/internal/wsapi/handler.go:148` (`_ = userID`) is removed.
- Existing `apps/server/internal/wsapi/handler_test.go` tests still pass; one new test asserts that a request to `?channel=BAD-CHANNEL-ID` returns 404 with the envelope.

## Out of scope (deferred)

- The PRD §10 frame-based WS protocol (`subscribe`/`unsubscribe`/`send` typed frames). The current handler treats inbound frames as opaque payloads to broadcast; that contract is intentional per the parent plan's "richer frame-based subscribe protocol can layer on top later" note. A separate plan should pick this up when CLI/Web clients need it.
- Per-frame `channel_id` validation (since frames aren't parsed yet). Once typed frames land, the same `GetChannel` helper introduced here is the natural place for that check.

## Implementation steps

1. Introduce `connState{userID, channel string}` (private, in `wsapi`) and pass a pointer through `Handler` → `readLoop` so message-handling has access without changing function signatures elsewhere.
2. Replace `_ = userID` (`handler.go:148`) with `state.userID = userID` and remove the TODO comment.
3. Add a `ChannelLookup func(ctx context.Context, id string) (bool, error)` field to `Config`. In `main.go`, wire it to the `repository.GetChannel` (or `repository.ChannelExists`) helper. When `ChannelLookup` is nil (phase-0 / no-DB boot), skip the check — same conditional pattern the handler already uses for `*auth.TicketStore`.
4. Before `websocket.Accept`, if `cfg.ChannelLookup` is set and the requested channel is not the legacy `#general` placeholder, call the lookup. On `(false, nil)` return 404 + envelope; on `(_, err)` return 500 + envelope and log the error with the request ID once the access-log middleware is wired.
5. Update `apps/server/internal/wsapi/handler_test.go` to cover (a) channel-not-found → 404, (b) `userID` is reachable from `readLoop` (test via a hub-side observer or by exporting a small accessor for tests).

## Test plan

- `test_ws_upgrade_returns_404_for_unknown_channel` — covers the new validation path.
- `test_ws_upgrade_succeeds_for_known_channel` — regression guard; the seeded `#general` and a ULID channel created via `POST /api/channels` must both succeed.
- `test_ws_userid_bound_after_ticket_redemption` — covers the parent plan's AC-3.
- The existing `test_ws_redeems_valid_ticket_*` cases must continue to pass unchanged.

## Files expected to be touched

- `apps/server/internal/wsapi/handler.go` (connState, Config.ChannelLookup, validation call site)
- `apps/server/internal/wsapi/handler_test.go` (new tests + adjustments)
- `apps/server/main.go` (wire `cfg.ChannelLookup = repository.ChannelExists` once the repo helper exists)
- `apps/server/internal/repo/channels.go` (`ChannelExists(ctx, id)` if not already exposed — `ListChannels` exists, but a single-row helper is cheaper for the WS path)

## Risks

- A 404 on the WS upgrade path differs from the AC's literal text ("rejected with a typed error frame"). Per the parent plan's own implementation notes, RFC 6455 close codes are only meaningful post-upgrade — pre-upgrade rejections must use HTTP. The frame-based variant lands when typed frames do.
- Adding a DB call to the upgrade hot path has cost. Mitigated by SQLite's local-call latency and (optionally) a small process-local LRU keyed by channel ID once it matters.
