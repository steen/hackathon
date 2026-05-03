# Changelog

All notable changes to Discord Lite are recorded here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

This changelog is intentionally **high-level**: meaningful product, architectural, or operational changes only — not every commit.

## [Unreleased]

### Added
- Initial Product Requirements Document at `specs/PRD.md`.
- Specifications live in a top-level `specs/` directory.
- Architectural decision: monorepo via pnpm workspaces (JS/TS) + `go.work` (Go modules).
- Architectural decision: web UI on Vite + React + TypeScript, chosen for first-class WebRTC support via LiveKit's React SDK.
- Architectural decision: SQLite for MVP with a documented PostgreSQL upgrade path (mirrored migrations, repository interfaces, dialect-portable SQL).
- Architectural decision: hub abstraction (in-proc today, NATS/Redis future) and `origin_server_id` on every message to prepare for federation.
- Architectural decision: opaque-payload message envelope (`payload`, `nonce`, `sender_key_id`, `recipient_wraps`) so the server cannot read message contents, enabling future E2E encryption without a server-side change.
- Architectural decision: ULIDs for all entity IDs.
- Testing stance: coverage is measured against requirements (user stories and functional requirements), not lines of code. Tests are tagged with requirement IDs.

### Planned (next)
- Phase 0 — walking skeleton: server + two CLI clients exchanging real-time messages, validated by `scripts/smoke.sh`.
- Phase 1 — persistence, auth, full message envelope.
- Phase 2 — TUI and Web UI.
- Phase 3 — polish, requirement-coverage report, demo build.

## [0.0.0] - 2026-05-03

### Added
- Repository initialized.
