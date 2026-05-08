# Feature: Channel rename endpoint (PATCH /api/channels/{id})

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

Channels are immutable today: `POST /api/channels` creates, `GET /api/channels` lists. There is no rename path on the wire. PRD §10 (post-#835) introduces `PATCH /api/channels/{id}` so users can fix typos and re-organize topics without losing message history (rows in `messages.channel_id` are unaffected by a rename — the channel ULID is stable; only `name` changes).

The seeded `#general` channel must remain stable: it is the default landing channel, hard-coded in the smoke script and the README. Renaming it would silently invalidate those entry points.

## Goal

Land the server-side `PATCH /api/channels/{id}` handler + repo + service code so the rest of Phase 8 (CLI, Web UI, WS broadcast) has a working endpoint to drive.

## Approach

1. Repo: add `UpdateChannelName(ctx, id, name) (Channel, error)` to `apps/server/internal/repo/channels.go`. Reuse the existing duplicate-name unique constraint to surface 409 via a typed `ErrChannelNameTaken` (the same error type used by `CreateChannel`).
2. Service / handler: add a `PATCH /api/channels/{id}` route in `apps/server/internal/wiring/channels.go` (or whichever wiring file already registers the channel routes — read first, do not invent). The handler:
   - Authenticates via the existing bearer middleware.
   - Reads `{ "name": "..." }` from the body, applies the same name validator as create (lowercase, hyphenated, length cap; reuse the existing helper rather than duplicate the rules).
   - Returns 403 with `error.code = "CHANNEL_PROTECTED"` (or the existing forbidden-code convention — check `apps/server/internal/http/errors` first) when `id` resolves to the seeded `#general` channel. The check is by name, not by ULID, so reseeding on a fresh DB still works.
   - Returns 404 when the channel does not exist.
   - Returns 409 on duplicate name (typed error from the repo).
   - Returns 200 with the updated `Channel` envelope on success.
3. Rate limit: PATCH and POST share a per-user token bucket — see `20-feature-channel-write-rate-limit.md`. This feature wires the route through that middleware; it does not own the limiter.
4. WS broadcast: this feature emits an internal event the hub subscribes to; the actual `channel` frame contract lives in `30-feature-channel-ws-events.md`. Keep the coupling to a single function call (`hub.BroadcastChannel(kind, ch)`); do not encode the frame shape here.

## Acceptance criteria

- `PATCH /api/channels/{id}` with a valid bearer token, valid name, and non-`#general` target returns 200 with the updated channel envelope.
- `PATCH /api/channels/{id}` against the `#general` channel returns 403 with the standard error envelope.
- `PATCH /api/channels/{id}` with a name that already exists on another channel returns 409.
- `PATCH /api/channels/{id}` with an invalid name (rules per `feature-channels-and-messages.md`) returns 400.
- `PATCH /api/channels/{id}` against a non-existent channel returns 404.
- The rename persists across server restart (covered via the repo test).
- After a successful rename, the in-memory hub broadcasts a `channel` frame (assertion belongs in the WS-events feature spec; this feature only asserts that the broadcast helper is invoked exactly once on the success path).

## Out of scope

- The `channel` WS frame's wire shape and the hub's broadcast plumbing — see `30-feature-channel-ws-events.md`.
- The per-user write rate limit — see `20-feature-channel-write-rate-limit.md`.
- CLI command + Web UI — see specs `60`, `70`.
- Renaming the seeded channel via an admin override — explicitly deferred; no env var, no flag.

## Pointers

- `apps/server/internal/wiring/channels.go` — existing channel route registration (verify path before editing; do not invent the filename).
- `apps/server/internal/repo/channels.go` — existing repo with `CreateChannel`; mirror its error-typing pattern.
- `apps/server/internal/http/channels_handlers.go` — existing handler conventions (envelope, validation helpers).
- `specs/plans/phase-1/feature-channels-and-messages.md` — name-validation rules to reuse.
- PRD §10 Channels — the wire contract.
