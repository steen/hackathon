# Implementation Plans

These plans were originally seeded from the PRD by an Archon workflow; ongoing maintenance and per-feature implementation is now driven by Claude Code subagents and agent teams (impl/bull/qual review loop). Update this README and the relevant phase/feature files manually as the PRD evolves.

Some Phase-0 features have a deeper `feature-name/test-plan.md` next to the feature file. This is a per-feature exception for AC-tagged exhaustive test specs and is not the convention for other phases.

| Phase | Title | Status | Plan |
|-------|-------|--------|------|
| 0 | Walking skeleton, system test ready | planned | [phase-0-walking-skeleton-system-test-ready](phase-0-walking-skeleton-system-test-ready.md) |
| 1 | Persistence + auth | planned | [phase-1-persistence-auth](phase-1-persistence-auth.md) |
| 2 | Web UI + shared clients | planned | [phase-2-web-ui-shared-clients](phase-2-web-ui-shared-clients.md) |
| 3 | Polish, demo | planned | [phase-3-polish-demo](phase-3-polish-demo.md) |

---

## Phase 0: Walking skeleton, system test ready
Status: planned · [Phase plan](phase-0-walking-skeleton-system-test-ready.md)

Features:
- [Monorepo scaffold](phase-0/feature-monorepo-scaffold.md)
- [Server WebSocket endpoint with in-memory hub](phase-0/feature-server-ws-hub.md)
- [CLI `chatd send` and `chatd watch` (no auth)](phase-0/feature-cli-send-watch.md)
- [System smoke test (`scripts/smoke.sh`)](phase-0/feature-smoke-test.md)

## Phase 1: Persistence + auth
Status: planned · [Phase plan](phase-1-persistence-auth.md)

Features:
- [SQLite schema and ULID generation](phase-1/feature-sqlite-schema-and-ulid.md)
- [Auth internals (bcrypt + JWT + password policy)](phase-1/feature-auth-internals.md)
- [Auth endpoints (register, login, me, logout, ws-ticket)](phase-1/feature-auth-endpoints.md)
- [Channels and messages endpoints (REST + WS)](phase-1/feature-channels-and-messages.md)
- [Startup config checks (JWT secret, bind, invite)](phase-1/feature-startup-config-checks.md)
- [Rate limits (per-IP login/register, per-username login backoff)](phase-1/feature-rate-limits.md)
- [Body and WS read/send caps](phase-1/feature-body-and-ws-caps.md)
- [WS hardening (origin check, ws-ticket flow, channel validation)](phase-1/feature-ws-hardening.md)
- [Access-log middleware and user-safe error envelope](phase-1/feature-logging-and-error-envelope.md)
- [SQLite file permissions and response security headers](phase-1/feature-file-perms-and-headers.md)

## Phase 2: Web UI + shared clients
Status: planned · [Phase plan](phase-2-web-ui-shared-clients.md)

Features:
- [`packages/go-client` (HTTP + WS client)](phase-2/feature-go-client-package.md)
- [CLI full command set (channels, history, login, watch, send)](phase-2/feature-cli-full-commands.md)
- [`packages/api-client` (TypeScript HTTP + WS + shared types)](phase-2/feature-ts-api-client-package.md)
- [`apps/web` (Vite + React + TS chat page)](phase-2/feature-web-app.md)
- [Presence (online users)](phase-2/feature-presence.md)

## Phase 3: Polish, demo
Status: planned · [Phase plan](phase-3-polish-demo.md)

Features:
- [README quick start](phase-3/feature-readme-quick-start.md)
- [Embedded web build into Go binary](phase-3/feature-embedded-web-build.md)
- [Seed `#general` channel](phase-3/feature-seed-general-channel.md)
- [Single-binary demo path verified](phase-3/feature-single-binary-demo-verified.md)
- [CHANGELOG entry for `0.1.0`](phase-3/feature-changelog-entry.md)
