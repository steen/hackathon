# Changelog

All notable changes to Discord Lite are recorded here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) (with timestamped sections instead of `[Unreleased]`) and the project uses [Semantic Versioning](https://semver.org/).

This changelog is intentionally **high-level**: meaningful product, architectural, or operational changes only — not every commit. Each merged PR with user-visible or operational impact gets its own timestamped section, newest first.

## Planned (next)
- Phase 0 — walking skeleton: server + two CLI clients exchanging real-time messages, validated by `scripts/smoke.sh`.
- Phase 1 — persistence, auth, full message envelope.
- Phase 2 — TUI and Web UI.
- Phase 3 — polish, requirement-coverage report, demo build.

## 2026-05-03 15:20Z — Server `/ws` endpoint with in-memory hub (phase 0)

### Added
- `apps/server/main.go` boots an HTTP server on `CHAT_SERVER_PORT` (default `8080`) with graceful shutdown on SIGINT/SIGTERM.
- `apps/server/internal/hub` provides per-channel pub/sub fan-out with `Subscribe` / `Unsubscribe` / `Broadcast`. Broadcast snapshots the subscriber set under a read lock so a slow subscriber cannot stall the hub.
- `apps/server/internal/wsapi` exposes the `/ws` HTTP handler. Each accepted connection auto-subscribes to `#general` for its lifetime; reads broadcast as text frames; writes drain through a 64-slot buffered queue (overflow drops messages for that subscriber rather than blocking the hub).

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
