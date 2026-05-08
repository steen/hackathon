# Feature: Per-user channel-write rate limit

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

`POST /api/channels` and `PATCH /api/channels/{id}` are cheap server-side, but a single authenticated user can use them to spam create/rename and either flood the WS `channel` broadcast to every connected client or churn the channel list. Existing rate-limit middleware (`feature-rate-limits.md`) is keyed on **source IP** and only covers the auth surface — login + register. Channel writes need per-user bucketing because friends often share a NAT'd IP, and we want one bad actor's channel-write spam to throttle that user without affecting their housemates.

PRD §9 introduces two env vars for this limiter:

- `CHAT_CHANNEL_WRITE_BURST` (default `10`) — token-bucket capacity.
- `CHAT_CHANNEL_WRITE_REFILL` (default `1m`) — interval at which one token is added.

The defaults give a fresh user 10 writes immediately, then one extra write per minute. That covers normal "I'm setting up channels for the weekend" usage without giving a runaway script room to flood.

## Goal

Add a per-user token-bucket limiter and apply it to the channel-write surface (`POST /api/channels`, `PATCH /api/channels/{id}`).

## Approach

1. New file: `apps/server/internal/ratelimit/userwrite.go` (the package already exists per `feature-rate-limits.md`'s `iplimit.go` / `userlimit.go` pair). Implement a token-bucket keyed by user ULID, with bounded LRU eviction so a long-running server with many users does not grow without bound.
2. Wire two config fields onto the existing `Config` struct and parse them in `apps/server/internal/config/config.go`:
   - `ChannelWriteBurst int` (default 10).
   - `ChannelWriteRefill time.Duration` (default `time.Minute`).
   Add `EnvChannelWriteBurst = "CHAT_CHANNEL_WRITE_BURST"` and `EnvChannelWriteRefill = "CHAT_CHANNEL_WRITE_REFILL"` consts so the existing `scripts/check-env-example.mjs` drift-check picks them up. Add the matching rows to `.env.example`.
3. New middleware: `apps/server/internal/http/middleware_channelwrite.go` that, given an authenticated request (the existing auth middleware injects `user_id` into the request context), pulls the bucket and either passes the call through or returns 429 with the standard error envelope (`error.code = "RATE_LIMITED"`, message generic).
4. Apply the middleware in the wiring file from `10-feature-channel-rename-endpoint.md` only on `POST` and `PATCH` — `GET /api/channels` and `GET /api/channels/{id}/messages` are NOT rate-limited by this layer.
5. Validation: refuse to start the server when `CHAT_CHANNEL_WRITE_BURST <= 0` or `CHAT_CHANNEL_WRITE_REFILL <= 0`. Reuse the existing `cfg.Validate()` plumbing.

## Acceptance criteria

- After `CHAT_CHANNEL_WRITE_BURST` consecutive writes from one user inside a single `CHAT_CHANNEL_WRITE_REFILL` window, the next write returns 429.
- The 429 response uses the standard `{ ok:false, error:{ code, message } }` envelope with `code = "RATE_LIMITED"`.
- After `CHAT_CHANNEL_WRITE_REFILL` elapses, one additional write succeeds.
- The limit applies to `POST /api/channels` and `PATCH /api/channels/{id}` and is shared between them (one bucket per user, not one per route).
- `GET /api/channels` is unaffected by this limiter.
- Two distinct users sharing one IP each have independent buckets — one user's 429 does not block the other.
- Server refuses to start with `CHAT_CHANNEL_WRITE_BURST=0` or a non-positive duration in `CHAT_CHANNEL_WRITE_REFILL`.
- `scripts/check-env-example.mjs` passes with the new env vars added to `.env.example`.

## Out of scope

- Persisting bucket state across server restarts — in-memory only, matches existing `iplimit.go` behavior.
- A per-channel rename limit. The bucket is per-user across the whole channel-write surface.
- Distributed rate-limiting (this is single-process by design; see PRD §13 for the federation/Postgres path).

## Pointers

- `apps/server/internal/ratelimit/iplimit.go` and `userlimit.go` — existing patterns to mirror for the bucket + LRU.
- `apps/server/internal/config/config.go` — `Env*` const block + `cfg.Validate()`.
- `scripts/check-env-example.mjs` — drift-check that ensures every server env-var const is mentioned in `.env.example`.
- PRD §9 (env-var table) for the CHAT_CHANNEL_WRITE_BURST / CHAT_CHANNEL_WRITE_REFILL rows.
- PRD §10 (Channels) — the 429 contract.
