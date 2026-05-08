# Feature: README quick start

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Status:** delivered (#70)

## Requirements covered
- (documentation supporting US-9 and US-10 hosting flow)

## Acceptance criteria
- `README.md` includes a "Quick start" section that takes a clean clone to a running app in under 5 minutes (matches Phase 3 validation criterion).
- Quick start documents the **server** env vars actually read by `apps/server` today (see `apps/server/internal/config/config.go` and `apps/server/main.go`):
  - `CHAT_JWT_SECRET` — required when `CHAT_DB_PATH` is set; must be ≥32 ASCII bytes, not a single repeated char, ≥5 distinct bytes, not on the dev-default denylist (`change-me`, `secret`, `password`, …).
  - `CHAT_INVITE_CODE` — required; gates registration.
  - `CHAT_DB_PATH` — required; SQLite file path the server opens, migrates, and persists to. No default; the server refuses to boot if unset.
  - `CHAT_LISTEN_ADDR` — defaults to `127.0.0.1:8080`. Non-loopback hosts are rejected unless `CHAT_ALLOW_PUBLIC_BIND=1`.
  - `CHAT_ALLOW_PUBLIC_BIND` — set to `1` to allow non-loopback bind (e.g. `0.0.0.0:8080`).
  - `CHAT_ALLOWED_ORIGINS` — comma-separated list of allowed WS Origin patterns (default same-origin only).
  - `CHAT_SERVER_PORT` — legacy; replaces the port half of `CHAT_LISTEN_ADDR` for compatibility.
- Quick start documents the **client** env var separately: `CHAT_SERVER` is the CLI's base-URL override (consumed by `apps/cli`), not a server var.
- Quick start shows: `pnpm install` → `pnpm dev` → open browser → register with invite code → send a message.
- Mentions the single-binary build (`40-feature-single-binary-demo-verified.md`) and points the reader to it.

## Implementation steps
1. Draft README sections: project intro, quick start (dev), single-binary build, env-var reference, troubleshooting.
2. Verify each command in the quick start runs as written from a fresh clone.
3. Time the path end-to-end and trim friction (any step that blocks under 5 minutes).

## Test plan
- Manual: clean clone, follow the README steps, time to first message ≤ 5 min.

## Files expected to be touched or created
- `README.md`

## Risks
- README rot is the most common doc failure; mitigated by referring to scripts (`pnpm dev`, `scripts/smoke.sh`) rather than hand-typed shell incantations.
