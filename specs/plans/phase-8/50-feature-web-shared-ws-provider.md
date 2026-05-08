# Feature: Web shared WebSocket provider

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

`apps/web` today opens its WebSocket inside the chat page. Phase 8's `channel` outbound frame (see `30-feature-channel-ws-events.md`) needs to update the channel list everywhere — sidebar, modal selectors, and the active-channel header. The current "WS lives in the chat page" shape forces the channel-list state into the same tree, which breaks once the create/rename modals (which can be opened from outside the chat page tree) need to know about new channels.

We need a single shared provider that:

1. Owns the WS connection lifecycle (open, reconnect with exponential backoff, close on logout).
2. Exposes typed subscriptions for `message`, `presence`, and `channel` frames.
3. Re-fetches `GET /api/channels` and `GET /api/presence` on (re)connect to seed snapshots.

The reconnect/backoff logic already exists in the chat page; this feature lifts it into a provider — it does NOT reinvent the algorithm.

## Goal

`apps/web/src/ws/WsProvider.tsx` (or equivalent path under `apps/web/src/`) exposes a context + hooks that any component in the tree can use to read connection state and subscribe to frames.

## Approach

1. New provider component `<WsProvider>` mounted once, just below `<AuthProvider>` in `apps/web/src/main.tsx` (verify the file's actual layout before editing — it may be `App.tsx` or a routes file).
2. Provider owns:
   - The `WebSocket` instance (one at a time, reopened on reconnect).
   - Connection-state machine: `connecting | open | reconnecting | closed`.
   - Subscriber registries keyed by frame `type`.
   - Snapshot refresh after every transition into `open` (re-fetch channels + presence).
3. Hooks exposed:
   - `useWsState(): WsConnectionState` — connection-state reads.
   - `useWsSubscribe(type, handler)` — register/unregister a typed handler. Handlers receive parsed `data` only; type-narrowing comes from the type parameter.
   - `useChannels(): Channel[]` — sugar that subscribes to `channel` frames internally and keeps a local snapshot in sync; the create/rename modals consume this.
4. Reconnect: keep the existing exponential-backoff schedule (PRD §"UX": "Web auto-reconnects within 5 s after a server restart"). On a `1008` close, do NOT auto-reconnect — that signals a policy violation (e.g. WS read limit exceeded) and reconnecting would loop.
5. Channel events: when a `channel` frame arrives, update the channel-list snapshot:
   - `kind: "create"` — append if not already present.
   - `kind: "rename"` — find by `id` and replace.
6. Logout: provider closes the WS and clears subscribers when the auth context flips to logged-out.

## Acceptance criteria

- Provider opens exactly one WebSocket per logged-in session; logout closes it.
- Reconnect uses exponential backoff capped consistent with the existing chat-page logic; the cap value carries over unchanged.
- After a (re)connect transitions to `open`, both `GET /api/channels` and `GET /api/presence` are fetched once before any consumer reads `useChannels()` for the new state.
- A `channel` frame with `kind: "create"` adds the channel to the snapshot exposed by `useChannels()`; the new entry is observable in a consumer within one render cycle.
- A `channel` frame with `kind: "rename"` updates the matching entry in `useChannels()` (matched by `id`).
- A `1008` close stops auto-reconnect.
- Subscribers registered via `useWsSubscribe` are removed on component unmount (no leaked handlers).

## Out of scope

- Multi-channel WebSocket connections — PRD §10 keeps one channel per WS.
- Replaying missed frames after reconnect (snapshot refresh covers it).
- Server-Sent Events fallback.
- Persisting connection state across page reloads (we re-open on mount).

## Pointers

- `apps/web/src/main.tsx` (or `App.tsx`) — current provider mount tree; verify before editing.
- The current chat-page WS hook — source of the existing reconnect/backoff logic to lift.
- `packages/api-client` (TS) — WS open helper + frame types (extended in `80-feature-clients-channel-extensions.md`).
- PRD §"UX" — reconnect-within-5s requirement.
- PRD §10 (WebSocket subsection) — frame contract.
