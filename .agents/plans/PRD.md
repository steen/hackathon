# Discord Lite — Product Requirements Document

## 1. Executive Summary

Discord Lite is a self-hosted, text-only chat application for friend groups uncomfortable with mandatory age verification and identity collection. The MVP ships two clients — a scriptable CLI and a React web app — backed by a single Go server with SQLite persistence.

**MVP goal**: by EOD, a friend can clone, build, run a single binary, and exchange real-time messages with the group from either client, with messages persisted across restarts.

**System-test-first**: Phase 0 stands up the server and two CLI clients exchanging real-time messages within the first hour, before any other feature work. Every later phase preserves this end-to-end testability.

This PRD describes only what ships in MVP. Federation, end-to-end encryption, encryption at rest, and Postgres are **not** prepared for in code — they are listed in §13 as work that will require real schema and architecture changes at the time they ship. Designing for them now would be speculative complexity for a one-day project.

## 2. Mission

Privacy-respecting, friend-scale chat that you fully control — accessible from terminal, script, or browser without ceding personal data or identity verification to a third party.

**Core principles**

1. **Privacy-first** — no third-party services, no telemetry, no PII beyond a chosen username.
2. **Multi-client, one API** — the server has no preferred client; CLI and Web use the same wire protocol.
3. **Boring and durable** — stdlib-first Go, minimal dependencies, immutable domain values, ULIDs.
4. **Ship the thing in front of you** — design for what's in this PRD, not for hypothetical futures.

## 3. Target Users

**Primary persona — "The privacy-conscious friend group"**

- 5–15 technically comfortable adults (developers / power users).
- Currently using Discord, concerned about its direction on age verification and identity collection.
- One member is willing to host on a personal machine or homelab.
- Need real-time text chat, multiple channels, history, and the freedom to use whichever client fits the moment (terminal during work, browser otherwise, script for automations).

**Technical comfort**: high. Comfortable with a binary release, env vars, and `curl`-style debugging. Not the audience for an installer wizard.

**Key needs**

- Trusted-group communication without third-party identity gates.
- Same conversation accessible from CLI and web.
- Local-first; bring-your-own-network. Predictable, inspectable behavior.

## 4. MVP Scope

### In Scope

**Core functionality**
- User registration and login (username + password)
- Persistent text channels (create + list)
- Real-time message send/receive over WebSocket
- Per-channel message history (paginated)
- Online presence (who is currently connected)
- Default `#general` channel seeded on first run

**Technical**
- Go HTTP + WebSocket server, single binary, embedded web assets
- SQLite via `modernc.org/sqlite` (CGO-free build)
- JWT auth (Bearer for HTTP, `?token=` query for WebSocket upgrade)
- Standard response envelope `{ ok, data, error }`
- Shared Go client used by CLI
- Shared TS client used by Web

**Clients**
- CLI (`chatd`) — login, channels, send, history, watch
- Web — Vite + React + TS, served by the Go server

**Deployment**
- Single Go binary with embedded web build
- Local-only binding (`127.0.0.1:8080` default)
- One-command bring-up: `pnpm dev` (root) or `go run ./cmd/server` (server only)

### Out of Scope (deferred)

- Voice / video / screen share
- E2E encryption
- Encryption at rest
- Multi-server / federation
- TUI client
- Postgres (SQLite only — switch when we outgrow it)
- File / image / attachment upload
- Reactions, emoji, rich text, markdown rendering
- Threads, replies, edit/delete
- Roles, permissions, moderation
- Multiple "guilds" — single server, multiple channels only
- Search across history
- Push notifications / unread badges
- Bots, webhooks, third-party integrations
- TLS / public hosting / domain (use a reverse proxy if exposed)
- Account recovery / email / 2FA
- Mobile native clients
- Horizontal scaling

## 5. User Stories

Each story has an ID (`US-N`) used to tag tests and demo steps. A story is "covered" when at least one passing test or scripted demo step references its ID.

- **US-1** — As a friend, I want to register an account, so I can join the chat.
- **US-2** — As a friend, I want to log in with my username and password, so I can resume conversations.
- **US-3** — As a user, I want to see the list of channels, so I can pick where to talk.
- **US-4** — As a user, I want to create a channel, so we can split topics.
- **US-5** — As a user, I want to send a message into a channel and have every connected client see it in real time, so chat feels live.
- **US-6** — As a user, I want to see prior messages when I open a channel, so I can catch up.
- **US-7** — As a user, I want to see who is currently online, so I know whether it's worth pinging now.
- **US-8** — As a scripter, I want a CLI command, so I can pipe automated notifications into chat.
- **US-9** — As a non-terminal user, I want a web UI, so I don't need to install anything.
- **US-10** — As the host, I want a single binary with env-var config, so deploying for friends is trivial.

## 6. Core Architecture & Patterns

### High-level architecture

```
┌──────────┐  ┌──────────────┐
│   CLI    │  │   Web UI     │
│ (cobra)  │  │ (Vite +      │
│          │  │  React + TS) │
└────┬─────┘  └──────┬───────┘
     │               │
 packages/go-client  packages/api-client (TS)
     │               │
     └─── HTTP + WebSocket ───┐
                              │
                     ┌────────┴────────┐
                     │   Go server     │
                     │   (chi router)  │
                     ├─────────────────┤
                     │   handlers      │  thin transport
                     │   services      │  business logic
                     │   hub            │  in-process broadcast
                     │   sqlite repo   │  data access
                     └────────┬────────┘
                              │
                         ┌────┴────┐
                         │ SQLite  │
                         └─────────┘
```

### Monorepo layout (pnpm workspaces + go.work)

```
hackathon/
├── apps/
│   ├── server/             # Go module — main HTTP+WS server
│   │   ├── cmd/server/main.go
│   │   ├── internal/api/   # handlers, middleware, hub
│   │   ├── internal/service/
│   │   ├── internal/repo/  # SQLite implementation
│   │   ├── go.mod
│   │   └── package.json    # scripts wrap go commands
│   ├── cli/                # Go module — chatd
│   └── web/                # Vite + React + TS — pnpm package
├── packages/
│   ├── go-client/          # Go module — HTTP+WS client (CLI)
│   └── api-client/         # TS package — HTTP+WS client + types (Web)
├── migrations/
│   └── 0001_init.sql
├── go.work
├── pnpm-workspace.yaml
├── package.json            # root scripts: dev, build, test
├── CHANGELOG.md
├── README.md
└── .agents/plans/PRD.md
```

### Key patterns

- **ULIDs everywhere.** Users, channels, messages all use ULID strings (lexicographically sortable, no auto-increment).
- **Standard envelope.** Every JSON response is `{ ok, data, error }`.
- **Immutability.** Domain values are passed by value; updates return new copies.
- **Small files.** 200–400 lines typical, 800 max.
- **Validation at boundaries.** Handlers validate inbound payloads; services trust their inputs.
- **Parameterized SQL only.** No string concatenation in queries.

## 7. Tools / Features

### Server (`apps/server`, binary `chat-server`)
- HTTP REST API: auth, channels, message history.
- WebSocket: real-time send/receive, presence, channel subscribe/unsubscribe.
- SQLite persistence.
- Embedded web build via `embed.FS`.
- Auto-migrate on start.
- Graceful shutdown on SIGINT/SIGTERM.

### CLI (`apps/cli`, binary `chatd`)
Built with cobra. Token cached at `~/.chatd/token`. Commands:

| Command | Purpose |
|---|---|
| `chatd register <username>` | Prompts for password |
| `chatd login <username>` | Prompts for password; stores token |
| `chatd whoami` | Prints current user |
| `chatd channels` | Lists channels |
| `chatd channels create <name>` | Creates a channel |
| `chatd send <channel> <msg>` | Sends a message (`-` reads stdin) |
| `chatd history <channel> [--limit N]` | Prints recent messages |
| `chatd watch <channel>` | Live tail of a channel (used in Phase 0 system test) |
| `chatd logout` | Clears stored token |

### Web UI (`apps/web`)
Vite + React 18 + TypeScript + Zustand + Tailwind.

- Login / register page.
- Single chat page: sidebar (channels + presence) + message list + input.
- Real-time updates via WebSocket.
- Reconnect-on-disconnect with exponential backoff.
- Reuses `packages/api-client` (TS) for all API calls.

**Why React + Vite (not HTMX, not SvelteKit)**: hackathon velocity. Zustand fits chat state cleanly, Vite is zero-config, the team already knows React.

## 8. Technology Stack

### Backend (Go 1.22+)

| Package | Purpose |
|---|---|
| `github.com/go-chi/chi/v5` | HTTP routing |
| `nhooyr.io/websocket` | Modern WebSocket |
| `modernc.org/sqlite` | Pure-Go SQLite driver |
| `github.com/pressly/goose/v3` | Migrations |
| `github.com/oklog/ulid/v2` | ULID generation |
| `github.com/golang-jwt/jwt/v5` | JWT issue/verify |
| `golang.org/x/crypto/bcrypt` | Password hashing |
| `github.com/stretchr/testify` | Test assertions |

### CLI

| Package | Purpose |
|---|---|
| `github.com/spf13/cobra` | CLI commands |
| `golang.org/x/term` | Password prompts |

### Web

| Package | Purpose |
|---|---|
| Vite | Build tool |
| React 18 | UI framework |
| TypeScript | Types |
| Zustand | State |
| Tailwind CSS | Styling |

### Tooling

| Tool | Purpose |
|---|---|
| `pnpm` (workspaces) | JS package management + monorepo orchestration |
| `go.work` | Go multi-module workspace |
| `goose` | Database migrations |
| `golangci-lint` | Go static analysis |
| `eslint` + `prettier` | TS/JS lint and format |

## 9. Security & Configuration

### Authentication

- Username + bcrypt-hashed password (cost 10).
- JWT (HS256) signed with `CHAT_JWT_SECRET`; 7-day TTL.
- HTTP: `Authorization: Bearer <token>`.
- WebSocket: `?token=<jwt>` on upgrade URL (browsers can't set headers on `WebSocket`).

### Configuration (env vars)

| Var | Default | Notes |
|---|---|---|
| `CHAT_LISTEN_ADDR` | `127.0.0.1:8080` | Local-only by default |
| `CHAT_DB_PATH` | `./chat.db` | SQLite file path |
| `CHAT_JWT_SECRET` | *(required)* | ≥32 bytes; server refuses to start without it |
| `CHAT_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Security in MVP scope

- bcrypt password hashing.
- Parameterized SQL only.
- JWT validation on every protected route and WS upgrade.
- WebSocket origin check against `CHAT_LISTEN_ADDR` host.
- Login rate limit (in-memory, 10 attempts / 5 min / source IP). Keyed on IP, not username, so failed attempts cannot be used to enumerate accounts.
- Login error messages do not leak whether the username exists.
- Server logs never include passwords or JWTs.

### Security out of MVP scope

- TLS — local deploy only.
- E2E encryption.
- Encryption at rest.
- 2FA, account recovery, email verification.

## 10. API Specification

Base URL: `http://127.0.0.1:8080`. All JSON responses use the standard envelope.

```json
{ "ok": true,  "data": <payload>, "error": null }
{ "ok": false, "data": null,      "error": { "code": "string", "message": "string" } }
```

### Auth

```
POST /api/auth/register   { "username": "...", "password": "..." }
                          → { "token": "...", "user": { "id", "username" } }

POST /api/auth/login      { "username": "...", "password": "..." }
                          → { "token": "...", "user": { ... } }

GET  /api/auth/me         (Bearer)
                          → { "user": { ... } }
```

### Channels

```
GET  /api/channels                      → [ { "id", "name", "created_at" } ]
POST /api/channels        { "name" }    → { "id", "name", "created_at" }
```

### Messages

```
GET  /api/channels/{id}/messages?limit=50&before=<msg_id>
                                        → [ { "id", "channel_id", "sender_user_id", "body", "created_at" } ]
POST /api/channels/{id}/messages
                          { "body" }    → <Message>
```

### WebSocket

```
GET /ws?token=<jwt>
```

Inbound (client → server):
```json
{ "type": "subscribe",   "channel_id": "..." }
{ "type": "unsubscribe", "channel_id": "..." }
{ "type": "send",        "channel_id": "...", "body": "..." }
```

Outbound (server → client):
```json
{ "type": "message",  "data": { "id", "channel_id", "sender_user_id", "body", "created_at" } }
{ "type": "presence", "data": { "channel_id", "users": [ { "id", "username" } ] } }
{ "type": "error",    "data": { "code", "message" } }
```

## 11. Success Criteria

**MVP success** = all 10 user stories pass; a friend on a clean machine can clone, build, log in via CLI or Web, and exchange messages in real time across both clients with messages persisted across restarts.

### Functional checks (each maps to a test or scripted demo step)

| ID | Check |
|---|---|
| US-1 | Register creates a user, returns a valid token |
| US-2 | Login with correct credentials returns a token; wrong creds return generic error |
| US-3 | `GET /api/channels` returns at least the seeded `#general` |
| US-4 | `POST /api/channels` creates and lists |
| US-5 | A message sent on one WS connection arrives on a second connection subscribed to the same channel within 500 ms |
| US-6 | History endpoint returns messages in created-at order with paging |
| US-7 | Connecting and disconnecting a client updates `presence` events within 2 s |
| US-8 | CLI `send` followed by CLI `watch` round-trips a message (smoke script) |
| US-9 | Web manual demo: login, send, receive across two browser windows |
| US-10 | Server boots from a clean directory with only env vars set |

### Quality gates

- `go test ./...` green across all Go modules.
- `pnpm -r test` green across TS packages.
- `scripts/smoke.sh` passes.
- Manual cross-client demo: CLI ↔ Web round-trip.
- One-command bring-up: `pnpm dev`.
- README documents quick start.

### UX

- Web does not block on send (optimistic render).
- Web auto-reconnects within 5 s after a server restart.
- Errors are user-friendly (no stack traces in client UI).

## 12. Implementation Phases

Total budget: ~8 hours.

### Phase 0 — Walking skeleton, system test ready (~1 hr)

**Goal**: server up, two CLI clients exchanging real-time messages over WebSocket. No auth, no DB, hardcoded `#general`. Prove the wire end-to-end.

Deliverables:
- Monorepo scaffold: `go.work`, `pnpm-workspace.yaml`, root `package.json` with `dev` / `build` / `test` scripts.
- `apps/server`: `/ws` endpoint with in-memory hub, broadcasts every received message to all subscribers of the channel.
- `apps/cli`: `chatd send` and `chatd watch` against `/ws` (no login).
- **System test**: `scripts/smoke.sh` boots server, runs two `chatd watch` processes, pipes a message via `chatd send`, asserts both watchers see it.

**Validation**: `scripts/smoke.sh` passes. This stays green for the rest of the project.

### Phase 1 — Persistence + auth (~2 hrs)

**Goal**: real users, channels, messages persisted to SQLite.

Deliverables:
- SQLite schema (`migrations/0001_init.sql`).
- ULID generation.
- `internal/auth`: bcrypt + JWT.
- Auth endpoints: register / login / me.
- Channels endpoints.
- Messages endpoints (REST + WS).
- Tests for US-1, US-2, US-3, US-4, US-5, US-6.

**Validation**: smoke test still green (now over authenticated WS).

### Phase 2 — Web UI + shared clients (~3.5 hrs)

**Goal**: React web client and full CLI against the same API.

Deliverables:
- `packages/go-client`: HTTP + WS client used by CLI.
- `apps/cli`: full command set (channels, history, login, watch, send).
- `packages/api-client` (TS): HTTP + WS client + shared types.
- `apps/web`: Vite + React + TS chat page; login/register; reconnect-on-disconnect with exponential backoff.
- Tests for US-7 (presence), US-8 (CLI round-trip).

**Validation**: manual cross-client demo (CLI ↔ Web message round-trip).

### Phase 3 — Polish, demo (~1.5 hrs)

Deliverables:
- README quick start.
- Embedded web build into Go binary.
- Seed `#general`.
- Single-binary demo path verified.
- CHANGELOG entry for `0.1.0`.

**Validation**: clean clone → `pnpm dev` → full demo flow under 5 minutes.

## 13. Future Considerations

Roadmap, in roughly the order they'd be tackled post-MVP. Each item below will require real schema and code changes at the time it ships — they are intentionally **not** prepared for in MVP code.

- **TUI client** — Bubble Tea three-pane reusing `packages/go-client`.
- **E2E encryption** — libsodium sealed boxes per channel; ratcheted session keys. Adds public keys to users, ciphertext envelope (`payload`/`nonce`/`sender_key_id`/`recipient_wraps`) to messages, and key-management UX to clients.
- **Encryption at rest** — SQLCipher build of SQLite, or volume-level encryption (LUKS, EBS) on the host.
- **Postgres** — when SQLite limits bite. Will need a repository abstraction, mirrored migrations, and a one-shot data migration tool.
- **Federation** — multi-server with NATS pub/sub. Adds `server_id`, `home_server_id`, and `protocol_version` to the schema and wire format; introduces a `Hub` interface with a NATS implementation.
- **A/V** — LiveKit room per channel for voice/video, gated by channel permission.
- **File / image attachments** — S3-compatible storage with signed URLs.
- **Reactions, replies, threads, edit/delete.**
- **Roles, permissions, moderation.**
- **Mobile** — React Native sharing `packages/api-client`.
- **Push notifications** — web push, APNs, FCM.
- **Search.**
- **Bots / webhooks.**

## 14. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Two clients in one day is tight | Medium | High | Phase 0 walking skeleton de-risks E2E early; shared `go-client` and `api-client` remove duplication; CLI is far smaller than Web and finishes first |
| Web UI bundling drags Phase 0 timing | Low | Medium | Phase 0 has no web; web work is Phase 2 |
| JWT secret leakage | Low | High | Required env var; server refuses to start without ≥32-byte secret; secret is never logged |
| WebSocket hub deadlocks / leaks | Medium | High | Per-connection bounded write channel; close on slow consumer; tests assert subscribe/unsubscribe count under churn |
| Scope creep into post-MVP features | High | High | This PRD is the contract. Anything in §13 stays out until §11 ships. |
| SQLite write-lock contention under multiple writers | Low | Low | Single-process server with serialized writes; friend-scale traffic is far below SQLite's threshold |

## 15. Appendix

### Related documents
- This PRD: `.agents/plans/PRD.md`
- Changelog: `CHANGELOG.md`
- README (Phase 3 deliverable): `README.md`

### Key dependency links
- pnpm: https://pnpm.io
- pnpm workspaces: https://pnpm.io/workspaces
- Go workspaces: https://go.dev/ref/mod#workspaces
- chi: https://github.com/go-chi/chi
- nhooyr.io/websocket: https://pkg.go.dev/nhooyr.io/websocket
- modernc.org/sqlite: https://pkg.go.dev/modernc.org/sqlite
- goose: https://github.com/pressly/goose
- cobra: https://github.com/spf13/cobra
- React: https://react.dev
- Vite: https://vite.dev
- Zustand: https://github.com/pmndrs/zustand
- ULID: https://github.com/ulid/spec

### Assumptions

These were inferred and should be flagged if any are wrong:

1. Single-server, single-host MVP.
2. Username + password is sufficient — no email, recovery, 2FA.
3. bcrypt + JWT are acceptable for a trusted-group local deploy.
4. SQLite via `modernc.org/sqlite` (no CGO) is acceptable for build simplicity.
5. Vite + React chosen for hackathon velocity, not for any future feature.
6. Coverage = "all user stories have a passing test or scripted demo step." We do not block on `go test -cover` percentages.
7. TLS, public hosting, account recovery deferred.
8. E2E encryption, encryption at rest, federation, and Postgres are explicitly **not** prepared for in code. They will require schema and code changes when they ship — that is the right time to design them.
