# Changelog

All notable changes to Discord Lite are recorded here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) (with timestamped sections instead of `[Unreleased]`) and the project uses [Semantic Versioning](https://semver.org/).

This changelog is intentionally **high-level**: meaningful product, architectural, or operational changes only — not every commit. Each merged PR with user-visible or operational impact gets its own timestamped section, newest first.

## Planned (next)
- Phase 0 — walking skeleton: server + two CLI clients exchanging real-time messages, validated by `scripts/smoke.sh`.
- Phase 1 — persistence, auth, full message envelope.
- Phase 2 — TUI and Web UI.
- Phase 3 — polish, requirement-coverage report, demo build.

## 2026-05-03 16:30Z — Body and WebSocket size/rate caps (phase 1)

### Added
- `apps/server/internal/httpx`: `Envelope` / `WriteError` matching the PRD §10 `{ok, data, error}` shape, plus a `BodyCap` middleware that caps every REST request at 16 KiB and writes a 413 `body_too_large` envelope on overflow (PRD §11 SEC-7). `WriteMessageTooLarge` provides the canonical 400 envelope for the REST chat-message path (SEC-8).
- `apps/server/internal/wsapi`: per-connection `SetReadLimit(64 KiB)` so the library closes oversize frames with WebSocket close code `1009` (SEC-6); 4 KiB cap on decoded message bodies (SEC-8 WS path) closes with `1009`; per-connection token bucket (10 msg/s, burst 30) closes flooding clients with `1008` (PRD §9).
- Tests assert the actual close codes observed by the client (`websocket.CloseStatus`), not just that the connection ended.

## 2026-05-03 15:30Z — chatd CLI binary entrypoint + smoke test (phase 0) (#18)

### Added
- `apps/cli/main.go` is now a `package main` dispatcher delegating to `apps/cli/cmd`. Supports `chatd [--url URL] send <msg...>` and `chatd [--url URL] watch`; URL falls back to `CHAT_SERVER` env or the default. Cancels on SIGINT/SIGTERM via `signal.NotifyContext`. The library shipped in #14; this lands the missing entrypoint.
- `scripts/smoke.sh` is the phase-0 system test: it builds `bin/server` and `bin/chatd`, picks a free port (via a one-shot `python3` socket bind, with `CHAT_SERVER_PORT` env override if `python3` is unavailable; the script errors out clearly if `python3` is missing and no override is set), boots the server, starts two `chatd watch` processes, sends a unique message via `chatd send`, and asserts both watchers received it within a 5s budget. Tears down all spawned processes via `trap cleanup EXIT INT TERM HUP` and dumps server/watcher logs only on failure.
- `pnpm smoke` shortcut and `pnpm test` (root) now runs the smoke script first, then the workspace test loop. Smoke first so a broken phase-0 system test fails fast before slower unit suites run, and so a missing workspace dep (e.g. unbuilt `tests/node_modules`) does not silently skip the canonical phase-0 acceptance test.
- CI: the `go` job in `.github/workflows/ci.yml` now runs `bash scripts/smoke.sh` after `go test`. The pnpm job uses `pnpm -r` which bypasses root scripts, so smoke is wired explicitly here rather than via `pnpm test`.

### Removed
- `apps/cli/doc.go` (its `package cli` declaration conflicted with the new `package main` in the same directory).

## 2026-05-03 15:20Z — Server `/ws` endpoint with in-memory hub (phase 0) (#17)

### Added
- `apps/server/main.go` boots an HTTP server on `CHAT_SERVER_PORT` (default `8080`, validated 1–65535) with `ReadHeaderTimeout`/`IdleTimeout` (Slowloris mitigation) and graceful shutdown on SIGINT/SIGTERM.
- `apps/server/internal/hub` provides per-channel pub/sub fan-out with `Subscribe` / `Unsubscribe` / `Broadcast`. Broadcast snapshots the subscriber set under a read lock so a slow subscriber cannot stall the hub.
- `apps/server/internal/wsapi` exposes the `/ws` HTTP handler. Default Origin verification is enforced (no `InsecureSkipVerify`). Each accepted connection auto-subscribes to `#general` for its lifetime; reads broadcast as text frames; writes drain through a 64-slot buffered queue (overflow drops messages for that subscriber rather than blocking the hub).

## 2026-05-03 13:05Z — CLI send/watch library (no-auth, phase 0) (#14)

### Added
- `apps/cli/cmd` package implementing `Send` (one-shot text frame to `/ws`) and `Watch` (read loop streaming text frames to an `io.Writer`) against a `coder/websocket` client; both perform a clean `StatusNormalClosure` handshake on parent-context cancellation.
- `ResolveURL` precedence: explicit flag → `CHAT_SERVER` env var → `ws://localhost:8080/ws`.
- AC-tagged test gate (`TestAC_0_4_NoAuthSymbolsReferencedFromCLI`) statically verifying no auth-related imports or literals leak into CLI sources during phase 0.

## 2026-05-03 11:59Z — Single-root Go module (#8)

### Changed
- Go module layout collapsed from `go.work` + per-app `go.mod` to a single root `go.mod` with module name `hackathon`. Imports use `hackathon/<path>`. The module name is intentionally decoupled from the GitHub coordinate so it survives org renames; the trade-off is that the module is not `go get`-able from outside the repo.

## 2026-05-03 11:45Z — Server WebSocket endpoint with in-memory hub (#6) — *later rolled back*

### Added
- `/ws` handler backed by a per-channel subscriber registry on `#general`, environment-driven config (`SERVER_PORT`, validated 1–65535), explicit `*http.Server` with `ReadHeaderTimeout` and `IdleTimeout` (Slowloris mitigation), clean WebSocket close (`StatusNormalClosure`).

### Removed
- The above merge was force-pushed off `main` shortly after landing; `apps/server/` reverted to its phase-0 stub. Functionality will land again as part of the next CLI/server PR.

## 2026-05-03 11:36Z — Monorepo scaffold (#5)

### Added
- `go.work` + `pnpm-workspace.yaml` + root `package.json` with `dev`/`build`/`test` fan-out scripts.
- GitHub Actions CI: per-module Go build/test, pnpm install/build/test, workflow-level concurrency group, least-privilege `contents: read` permissions.

## 2026-05-03 — Initial repository

### Added
- Initial Product Requirements Document at `specs/PRD.md`.
- Specifications live in a top-level `specs/` directory.
- Architectural decision: monorepo via pnpm workspaces (JS/TS) + a single root `go.mod` (Go).
- Architectural decision: web UI on Vite + React + TypeScript, chosen for first-class WebRTC support via LiveKit's React SDK.
- Architectural decision: SQLite for MVP with a documented PostgreSQL upgrade path (mirrored migrations, repository interfaces, dialect-portable SQL).
- Architectural decision: hub abstraction (in-proc today, NATS/Redis future) and `origin_server_id` on every message to prepare for federation.
- Architectural decision: opaque-payload message envelope (`payload`, `nonce`, `sender_key_id`, `recipient_wraps`) so the server cannot read message contents, enabling future E2E encryption without a server-side change.
- Architectural decision: ULIDs for all entity IDs.
- Testing stance: coverage is measured against requirements (user stories and functional requirements), not lines of code. Tests are tagged with requirement IDs.
- Repository initialized.
