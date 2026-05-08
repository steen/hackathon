# Feature: Client extensions (TS api-client + Go client) for channel rename + WS `channel` frame

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

Both clients carry hand-mirrored wire types. Per CLAUDE.md ("Wire types"):

> Wire types are hand-mirrored on both sides of the wire: `packages/go-client/{auth,channels,messages,ws,client}.go` (structs with `json:"..."` tags) and `packages/api-client/src/types.ts` (interfaces). Each file carries a top-of-file `sync with <counterpart>` comment. When adding a JSON field, change both files in the same PR and add an e2e assertion under `tests/e2e/` so drift fails CI.

Phase 8 adds two contract changes that need both sides to land together:

1. `PATCH /api/channels/{id}` — the rename HTTP call.
2. `{type:"channel", data:{kind, channel}}` — the new outbound WS frame.

This feature owns the client-side mirror for both. The CLI (`70-feature-channel-cli-commands.md`) and Web UI (`60-feature-channel-create-rename-ui.md`) consume these clients; landing the clients first means those features have a typed surface to call into.

## Goal

Extend `packages/go-client` and `packages/api-client` so:

- Both expose a `RenameChannel` / `renameChannel` method that issues `PATCH /api/channels/{id}`.
- Both expose a `ChannelEvent` type matching the new WS frame, and the WS plumbing dispatches `channel` frames to subscribers.

## Approach

1. `packages/go-client/channels.go` — add `RenameChannel(ctx, id, name string) (Channel, error)`. Mirror the existing `CreateChannel` shape (envelope unwrap, error decoding, 4xx → typed errors). Top-of-file `sync with packages/api-client/src/channels.ts` comment must list the new method.
2. `packages/api-client/src/channels.ts` (or wherever the channel HTTP calls live; verify) — add `renameChannel(id, body): Promise<Channel>` mirroring `createChannel`. The `sync with packages/go-client/channels.go` comment must list the new method.
3. `packages/go-client/ws.go` — add a `ChannelEvent` struct:
   ```go
   type ChannelEvent struct {
       Kind    ChannelEventKind `json:"kind"`    // "create" | "rename"
       Channel Channel          `json:"channel"`
   }
   ```
   Plus `const ChannelEventKindCreate ChannelEventKind = "create"` (and `…Rename`). Extend the WS read loop to dispatch `channel` frames to a registered handler (mirror the existing message/presence dispatch shape).
4. `packages/api-client/src/types.ts` — add `ChannelEvent` interface and a discriminated `WsFrame` variant for `type: "channel"`.
5. e2e: add an assertion under `tests/e2e/` (placement per `tests/e2e/` conventions — verify before adding) that:
   - Connects two clients (one Go, one TS) to the WS.
   - One issues `POST /api/channels`.
   - Both observe the `channel` frame with `kind: "create"`.
   - One issues `PATCH /api/channels/{id}`.
   - Both observe the `channel` frame with `kind: "rename"`.
   This is the drift-canary CLAUDE.md mandates ("add an e2e assertion under `tests/e2e/` so drift fails CI").

## Acceptance criteria

- `packages/go-client/channels.go` exposes `RenameChannel(ctx, id, name) (Channel, error)`; `sync with` comment is updated.
- `packages/api-client/src/channels.ts` exposes `renameChannel(id, body)`; `sync with` comment is updated.
- `packages/go-client/ws.go` defines `ChannelEvent` and routes `channel` frames to a registered handler.
- `packages/api-client/src/types.ts` defines `ChannelEvent` and the discriminated WS frame variant.
- An e2e test under `tests/e2e/` asserts both clients observe `channel` frames for create and rename. The test fails CI if the wire shapes drift between Go and TS.

## Out of scope

- A `Channel` type field rename or breaking change — `id`, `name`, `created_at` are unchanged.
- A presence frame change.
- A REST batching API for channel operations.
- Server-side work — that lives in `10`, `20`, `30`.
- CLI / Web consumers — those live in `60`, `70`.

## Pointers

- `packages/go-client/channels.go`, `messages.go`, `ws.go`, `client.go` — existing shape + `sync with` comments.
- `packages/api-client/src/types.ts`, `channels.ts` (or equivalent) — TS mirror.
- CLAUDE.md "Wire types" — the contract this feature observes.
- PRD §10 (Channels + WebSocket).
- `tests/e2e/` — existing e2e structure; verify before placing the new assertion.
