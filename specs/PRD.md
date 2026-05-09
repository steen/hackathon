# Discord Lite — Product Requirements Document

**Revision:** PR #128 (post-Phase-2 alignment per #126; supersedes commit `7e33be3`)

## 1. Executive Summary

Discord Lite is a self-hosted, text-only chat application for friend groups uncomfortable with mandatory age verification and identity collection. The MVP ships two clients — a scriptable CLI and a React web app — backed by a single Go server with SQLite persistence.

**MVP goal**: by EOD, a friend can clone, build, run a single binary, and exchange real-time messages with the group from either client, with messages persisted across restarts.

**System-test-first**: Phase 0 stands up the server and two CLI clients exchanging real-time messages within the first hour, before any other feature work. Every later phase preserves this end-to-end testability.

This PRD describes only what ships in MVP. Federation, encryption at rest, and Postgres are **not** prepared for in code — they are listed in §13 as work that will require real schema and architecture changes at the time they ship. Designing for them now would be speculative complexity for a one-day project. End-to-end encryption (channels + DMs) shipped Phase 10 — see §13 "Shipped post-MVP" and `specs/plans/phase-10/`.

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
- User registration (invite-code gated) and login (username + password)
- Persistent text channels (create + list)
- Real-time message send/receive over WebSocket
- Per-channel message history (paginated)
- Online presence (who is currently connected)
- Default `#general` channel seeded on first run
- Server-side logout (JWT invalidation via per-user token version)
- Auth audit log (register / login success / login fail / logout)
- Direct messages, 1:1 only (Phase 9)
- Server-tracked read state for channels and DMs (Phase 9)
- End-to-end encryption (channels + DMs) — NaCl `crypto_box` / `crypto_secretbox` / `crypto_sign`; static root key per channel/DM, wrapped per recipient; passphrase-derived per-user identity (Phase 10)
- Public-vs-private channels with explicit membership — `channel_members` table, inviter-signed membership rows, `channels.is_public` flag, `#general` auto-add (Phase 10)

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
- **US-11** — As the host, I want registration gated by an invite code, so a publicly reachable instance is not joinable by strangers.
- **US-12** — As a user, I want logout to actually invalidate my token server-side, so a stolen token stops working when I notice.
- **US-13** — As a user, I want to rename a channel (other than `#general`), so we can fix typos and reorganize topics without losing history.

## 6. Core Architecture & Patterns

### High-level architecture

```
┌──────────┐  ┌──────────────┐
│   CLI    │  │   Web UI     │
│ (stdlib  │  │ (Vite +      │
│  flag)   │  │  React + TS) │
└────┬─────┘  └──────┬───────┘
     │               │
 packages/go-client  packages/api-client (TS)
     │               │
     └─── HTTP + WebSocket ───┐
                              │
                     ┌────────┴────────┐
                     │   Go server     │
                     │ (net/http mux)  │
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

### Monorepo layout (pnpm workspaces + single-module Go)

```
hackathon/
├── go.mod                  # single root Go module, name `hackathon`; imports use `hackathon/<path>`
├── apps/
│   ├── server/             # main HTTP+WS server
│   │   ├── main.go         # (or cmd/server/main.go as the package grows)
│   │   ├── internal/hub/   # in-process pub/sub
│   │   ├── internal/wsapi/ # WebSocket handler + framing
│   │   ├── internal/repo/  # SQLite-backed data access (planned)
│   │   └── package.json    # scripts wrap go commands
│   ├── cli/                # chatd
│   └── web/                # Vite + React + TS — pnpm package
├── packages/
│   ├── go-client/          # HTTP+WS client (CLI), part of the `hackathon` module
│   └── api-client/         # TS package — HTTP+WS client + types (Web)
├── migrations/
│   └── 0001_init.sql
├── pnpm-workspace.yaml
├── package.json            # root scripts: dev, build, test
├── CHANGELOG.md
├── README.md
└── specs/PRD.md
```

The exact internal package layout under `apps/server/internal/` will firm up as features land in Phase 1; the names above are the planned shape rather than verified directories.

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
Built on stdlib `flag`. A small `splitFlagsAndPositional` helper (see `apps/cli/cmd/history.go`, landed in PR #117) lets per-command `flag.FlagSet` instances accept flags placed after positional args (`chatd history <chan> --limit 10`) without pulling in `pflag`. Token cached at `~/.chatd/token`. Commands:

| Command | Purpose |
|---|---|
| `chatd register <username>` | Prompts for password |
| `chatd login <username>` | Prompts for password; stores token |
| `chatd whoami` | Prints current user |
| `chatd channels` | Lists channels |
| `chatd channels create <name>` | Creates a channel; prints `<id>\t<name>` |
| `chatd channels rename <current-name> <new-name>` | Renames a channel; prints `<id>\t<name>` |
| `chatd send <channel> <msg>` | Sends a message (`-` reads stdin) |
| `chatd history <channel> [--limit N]` | Prints recent messages |
| `chatd watch <channel>` | Live tail of a channel (used in Phase 0 system test) |
| `chatd logout` | Clears stored token |

Future migration path: a move to cobra/pflag becomes worthwhile once subcommand groups, generated `--help`, or shell completion are needed. At 9 flat commands the stdlib `flag` + helper is cheaper to maintain.

### Web UI (`apps/web`)
Vite + React 18 + TypeScript. State via React Context; styling via plain CSS at `apps/web/src/styles.css`.

- Login / register page.
- Single chat page: sidebar (channels + presence) + message list + input.
- Real-time updates via WebSocket.
- Reconnect-on-disconnect with exponential backoff.
- Reuses `packages/api-client` (TS) for all API calls.

**Why React + Vite (not HTMX, not SvelteKit)**: hackathon velocity. Vite is zero-config, the team already knows React.

**Why Context + plain CSS (not Zustand + Tailwind)**: at the 4-route MVP scale neither is load-bearing. Revisit if cross-route state mutation or a design system becomes required (see §13).

## 8. Technology Stack

### Backend (Go 1.22+)

| Package | Purpose |
|---|---|
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
| `golang.org/x/term` | Password prompts |

### Web

| Package | Purpose |
|---|---|
| Vite | Build tool |
| React 18 | UI framework |
| TypeScript | Types |

### Tooling

| Tool | Purpose |
|---|---|
| `pnpm` (workspaces) | JS package management + monorepo orchestration |
| single root `go.mod` (`module hackathon`) | Single Go module covers all apps and packages; imports use `hackathon/<path>` |
| `goose` | Database migrations |
| `golangci-lint` | Go static analysis |
| `eslint` + `prettier` | TS/JS lint and format |

## 9. Security & Configuration

This is a security-critical app — a friend-group chat that may eventually sit behind a reverse proxy on the public internet. The MVP must not be the soft target.

### Threat model

- **In-scope attackers**: unauthenticated network attacker (port-scanning, scripted login/register attempts, XSS payloads in messages); logged-in user attempting to escalate, enumerate, flood, or deface; passive network attacker outside the deploy (TLS / WSS termination is mandated for any non-loopback deployment); **post-Phase 10:** malicious operator with full server access (DB snapshot, RAM, live process) — for MESSAGE CONTENT only. The Phase 10 encryption design (`specs/plans/phase-10/encryption.md`, `…/membership.md`, `…/keys.md`, `…/security.md`) defends private-channel and DM bodies against an operator-with-DB+RAM access; metadata, public-channel content, and the social graph remain readable to such an operator (see R1.2 + L19).
- **Trust boundary** (Phase 10): the wrap-list source of truth is the explicit `channel_members` relation. `#general` and any other `is_public = TRUE` channel auto-adds every authenticated user (operator-readable per R1.2). Private channels (`is_public = FALSE`, the default) are gated by inviter-signed membership rows; non-members cannot decrypt regardless of `CHAT_INVITE_CODE` possession. DMs are 1:1 — no third party can ever join.
- **Named residual risks (Phase 10 — accepted for v1)**:
  - **R1.1 — Modified server-delivered client.** A rogue operator can serve modified web JS or a modified `chatd` Go binary that exfiltrates the identity passphrase, fakes the wrong-passphrase canary check, or poisons the TOFU cache at first contact (the receiver caches whatever `sender_sign_pubkey` is in the first message they see). Per L40 the protocol-layer arm broadens to "rogue server with DB write access can swap stored `sender_sign_pubkey` + `signature` columns on a message envelope." Defenses (reproducible builds, pinned client checksums, code-signing, out-of-band fingerprint comparison) are out of scope for v1 — see `specs/plans/phase-10/security.md`.
  - **R1.2 — Operator-as-public-channel-member.** An operator who registers using their own `CHAT_INVITE_CODE` becomes a member of every channel marked `is_public = TRUE` and can read its messages once any existing member's client lazy-wraps for them. Private channels (the default) remain defended via §10 inviter-signed membership; the operator cannot inject themselves because their forged `channel_members` row has no valid signature. See `specs/plans/phase-10/security.md`.
- **Out of scope**: host compromise of an end-user device, nation-state breaking modern primitives, physical access, side-channel attacks on the host.

### Authentication

- Username + bcrypt-hashed password (cost 10, OWASP floor; tunable via `CHAT_BCRYPT_COST`).
- Password policy: minimum 10 characters. Bcrypt truncates input at 72 bytes — server rejects passwords > 72 bytes with a clear error rather than silently truncating.
- Constant-time login: on unknown username, server still runs a bcrypt compare against a fixed dummy hash, so response time does not enumerate accounts.
- Login error messages are generic ("invalid username or password") and identical for unknown-user and wrong-password cases.
- JWT (HS256) signed with `CHAT_JWT_SECRET`; 7-day TTL; claims include `sub` (user ID) and `tv` (token version).
- **Server-side revocation**: each user row has a `token_version` integer. JWT `tv` claim must equal current `token_version` or the token is rejected. `POST /api/auth/logout` and any future password change increment it.
- HTTP: `Authorization: Bearer <token>`. Tokens are **never** stored in cookies (eliminates CSRF on REST). Web client holds the token in memory; optionally mirrored to `localStorage` for reload survival.
- WebSocket: browsers cannot set `Authorization` on `WebSocket`. Auth flow:
  1. Client `POST /api/auth/ws-ticket` (Bearer) → returns single-use, 30s ticket bound to the user.
  2. Client opens `/ws?ticket=<ticket>`.
  3. Server consumes the ticket atomically (one-shot) and upgrades.
- The 7-day session JWT never appears in URLs, access logs, or browser history. The 30s ticket may, but its blast radius is one connection within 30 seconds.

### Registration gating

- Registration requires `CHAT_INVITE_CODE` env var to be set; client must present matching `invite_code` in the register payload.
- Without this, a publicly reachable instance is open to the internet. Server refuses to start with both `CHAT_INVITE_CODE` unset *and* `CHAT_ALLOW_PUBLIC_BIND=1`.

### Network exposure

- Default bind: `127.0.0.1:8080`.
- Server refuses to bind to a non-loopback address unless `CHAT_ALLOW_PUBLIC_BIND=1` is set explicitly. Prevents accidental exposure from a typo in `CHAT_LISTEN_ADDR`.
- WebSocket upgrade enforces same-origin by default (compares `Origin` header to the request host). `CHAT_ALLOWED_ORIGINS` (comma-separated) overrides for reverse-proxy deployments.
- Login rate limit is keyed on source IP; behind a proxy this collapses to one IP. `X-Forwarded-For` is honored **only** when `CHAT_TRUSTED_PROXY=1` is set.

### Rate limits & resource bounds

- **Login**: 10 attempts / 5 min / source IP (in-memory token bucket). Plus per-username delay (linear backoff up to ~2s) to slow targeted attacks without enabling lockout-DoS.
- **Registration**: 5 attempts / 15 min / source IP.
- **WebSocket read limit**: 64 KiB per frame (`Conn.SetReadLimit`).
- **Per-connection send rate limit**: 10 msg/s, burst 30. Excess frames drop the offending connection with a `1008` policy-violation close.
- **Message body cap**: 4 KiB, enforced in the handler before reaching the service. Same cap on REST and WS paths.
- **REST request body cap**: 16 KiB via `http.MaxBytesReader` on every handler.
- **WS `send`**: server validates the channel exists and rejects sends to non-existent channels rather than silently dropping.

### Input handling & rendering

- Parameterized SQL only — no string concatenation in queries.
- Validation at handler boundary; services trust their inputs.
- Message bodies are **rendered as plain text** in the web client. No `dangerouslySetInnerHTML`. No markdown. No auto-linkification in MVP. One missed escape on user-controlled text = stored XSS hitting every client.
- Server sets these response headers on all routes:
  - `Content-Security-Policy: default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
  - `X-Frame-Options: DENY`

### Secrets & config hygiene

- `CHAT_JWT_SECRET`: required, ≥32 ASCII bytes. Server refuses to start if it is missing, shorter than 32 bytes, or matches any of a hardcoded denylist of obvious dev defaults (e.g. `change-me`, `secret`, `dev`, repeated single chars).
- `CHAT_INVITE_CODE`: required when registration is enabled.
- Server logs never include passwords, JWTs, or tickets. Access-log middleware strips `token` and `ticket` query parameters before writing the log line.
- Error envelope `error.message` is user-safe — never SQL errors, stack traces, file paths, or driver messages. The `error.code` is a stable enum; details go to server logs only.

### Persistence hygiene

- SQLite file is created with mode `0600`; parent directory should be `0700`. Server logs a warning if it observes wider modes.
- README documents `sqlite3 chat.db ".backup chat.bak"` for cold backups.

### Audit log

- Append-only table `auth_events(id, user_id NULLABLE, username, event, source_ip, user_agent, created_at)` capturing: `register`, `login_success`, `login_failure`, `logout`, `token_version_bump`. No passwords, no tokens.

### Configuration (env vars)

| Var | Default | Notes |
|---|---|---|
| `CHAT_LISTEN_ADDR` | `127.0.0.1:8080` | Non-loopback requires `CHAT_ALLOW_PUBLIC_BIND=1` |
| `CHAT_ALLOW_PUBLIC_BIND` | `0` | Must be `1` to bind a non-loopback address |
| `CHAT_DB_PATH` | `./chat.db` | SQLite file path; created `0600` |
| `CHAT_JWT_SECRET` | *(required)* | ≥32 bytes; not in dev-default denylist |
| `CHAT_BCRYPT_COST` | `10` | OWASP floor; raise on faster hosts |
| `CHAT_INVITE_CODE` | *(required)* | Gate on registration; required when bound publicly |
| `CHAT_ALLOWED_ORIGINS` | *(same-origin)* | Comma-separated; for reverse-proxy deploys |
| `CHAT_TRUSTED_PROXY` | `0` | If `1`, honor `X-Forwarded-For` for rate-limit IP |
| `CHAT_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `CHAT_CHANNEL_WRITE_BURST` | `10` | Per-user token-bucket burst for channel-write endpoints (`POST /api/channels`, `PATCH /api/channels/{id}`) |
| `CHAT_CHANNEL_WRITE_REFILL` | `1m` | Per-user token-bucket refill interval for channel-write endpoints; one token per interval |

### Security explicitly out of MVP scope

- TLS — terminate at a reverse proxy if exposed; local deploy otherwise.
- Encryption at rest (SQLCipher / volume-level encryption). Decision-log L19 documents that SQLCipher does NOT solve metadata exposure (operator holds the key); MLS / Signal Sealed Sender / private-information-retrieval are the roadmap families to evaluate for metadata hiding.
- 2FA, account recovery (no server-side recovery for the Phase 10 identity passphrase — forgotten passphrase = account-wipe by design), email verification.
- Out-of-band fingerprint / safety-number / QR verification UX. Phase 10 ships TOFU + key-change warning only (decision-log L20).
- Per-message forward secrecy / epoch-rotated keys. Phase 10's static-root-key + member-removal-only rotation (L16) is the locked-in group-key model.
- Reproducible builds / signed binaries (R1.1 mitigation).

## 10. API Specification

Base URL: `http://127.0.0.1:8080`. All JSON responses use the standard envelope.

```json
{ "ok": true,  "data": <payload>, "error": null }
{ "ok": false, "data": null,      "error": { "code": "string", "message": "string" } }
```

### Auth

```
POST /api/auth/register   { "username": "...", "password": "...", "invite_code": "...",
                            "box_pubkey": "<base64 32>", "sign_pubkey": "<base64 32>" }
                          → { "token": "...", "user": { "id", "username", "box_pubkey", "sign_pubkey" } }

POST /api/auth/login      { "username": "...", "password": "..." }
                          → { "token": "...", "user": { "id", "username", "box_pubkey", "sign_pubkey" } }

POST /api/auth/logout     (Bearer)
                          → { "ok": true }                  # bumps token_version, invalidates all tokens for the user

GET  /api/auth/me         (Bearer)
                          → { "user": { "id", "username", "box_pubkey", "sign_pubkey" } }

POST /api/auth/ws-ticket  (Bearer)
                          → { "ticket": "...", "expires_at": "..." }   # single-use, 30s TTL, bound to user
```

### Channels

```
GET   /api/channels                      → { "channels": [ { "id", "name", "is_public", "created_at" } ] }
POST  /api/channels        { "name", "is_public"?, "membership", "root_key_wraps" }
                                         → { "id", "name", "is_public", "created_at" }   # 200; 400 invalid name; 409 duplicate; 429 per-user rate limit
PATCH /api/channels/{id}   { "name" }    → { "id", "name", "is_public", "created_at" }   # 200; 400 invalid name; 403 on the seeded `#general` channel; 409 duplicate; 429 per-user rate limit
```

`POST` and `PATCH` share a per-user token-bucket rate limit (`CHAT_CHANNEL_WRITE_BURST` / `CHAT_CHANNEL_WRITE_REFILL`, defaults `10` / `1m`). The seeded `#general` channel cannot be renamed. Name validation reuses the same shape rules as create (lowercase, hyphenated, length cap; see `feature-channels-and-messages.md`). Phase 10: `GET /api/channels` filters to channels the viewer is a member of (decision-log §6 + L25); `is_public` defaults to `false` on `POST` (Phase 10 §9); `is_public` is **immutable after creation** (L15 — no `PATCH` for the flag). `POST /api/channels` carries a self-signed `MembershipBlock` and a single wrap-to-self in `root_key_wraps` per `specs/plans/phase-10/membership.md` and `specs/plans/phase-10/encryption.md`.

### Messages

```
GET  /api/channels/{id}/messages?limit=50&before=<msg_id>
                                        → { "messages": [ <Message> ] }
POST /api/channels/{id}/messages
                          { "envelope" } → <Message>
```

Phase 10: the plaintext `body` field is removed and replaced by the signed `envelope` (see WebSocket section below for the envelope shape). The plaintext 4 KiB cap (PRD §9) is enforced **client-side** before encryption; server enforces only the existing 16 KiB REST body cap on the ciphertext payload (L17). Wire shape per `specs/plans/phase-10/encryption.md` (L21).

### Presence

```
GET  /api/presence        (Bearer)      → { "users": [ { "id", "username" } ] }
```

Returns the current set of connected users for reconciliation on (re)connect. WS `presence` frames carry only deltas; clients seed from this snapshot.

### WebSocket

```
GET /ws?ticket=<ws-ticket>&channel=<channel_id>
```

The ticket is obtained via `POST /api/auth/ws-ticket`. Tickets are single-use, 30-second TTL, and bound to the issuing user. The session JWT itself is never accepted as a query parameter.

Channel scope is set at upgrade via `?channel=<channel_id>`. Sends use REST `POST /api/channels/{id}/messages`. Multiplexing several channels on a single WS is intentionally not supported — eliminating client-supplied `channel_id` on inbound frames closes the sender-spoofing surface (see §"Design deviations" below). To follow another channel, the client opens a second connection with a new ticket.

Inbound (client → server): no application-level frames. The server reads only protocol-level pings/pongs and close frames.

Outbound (server → client):
```json
{ "type": "message",  "data": { "id", "channel_id", "sender_user_id", "envelope": { ... }, "created_at" } }
{ "type": "presence", "data": { "kind": "join" | "leave", "user_id": "..." } }
{ "type": "channel",  "data": { "kind": "create" | "rename" | "members_changed" | "key_received" | "wrap_failed", ... } }
{ "type": "dm",       "data": { "conversation": <Conversation>, "dm_message": <DMMessage> } }
{ "type": "read",     "data": { "scope": "channel" | "dm", "target_id": "...", "last_read_message_id": "...", "unread_count": 0 } }
{ "type": "error",    "data": { "code", "message" } }
```

Phase 10: the `message` and `dm` `data` blocks carry the signed `envelope` (`cipher_suite`, `key_generation_id`, `nonce`, `ciphertext`, `sender_sign_pubkey`, `signature`, `client_created_at`) instead of plaintext `body` — see `specs/plans/phase-10/encryption.md` (L21). The `channel` `data.kind` enum gains three Phase 10 entries: `members_changed` (broadcast to remaining members after a `POST`/`DELETE …/members/…`; carries `{channel_id, current_generation_id, members_at_rotation}`), `key_received` (fanned out to the receiving user's `user:<viewer>` topic when a wrap arrives; carries `{channel_id, generation_id}`), and `wrap_failed` (fired to the user's own `user:<viewer>` topic when `crypto_box_open` on their wrap fails; recovery via `replay-wrap` per `specs/plans/phase-10/keys.md`).

Phase 9 extends the topic model: every authenticated WS connection subscribes to **two** Hub topics for its lifetime — the channel topic (per the existing `?channel=<id>` upgrade parameter, or the legacy `defaultChannel = "#general"` fallback when omitted) AND the user-inbox topic `user:<viewer>` (decision §4, §10, L15). The `?channel=` parameter remains optional under the legacy default to preserve existing go-client/CLI behavior; new clients can pass an explicit channel id. Frames are routed by topic:

- `message` and `presence` frames flow over the channel topic (existing behavior — no change).
- `channel` frames remain global broadcasts via `Hub.BroadcastAll`.
- `dm` frames are emitted to `user:<sender>` and `user:<recipient>` after `POST /api/dms/{id}/messages`. The frame is **self-sufficient on first contact** (§8): the embedded `conversation` block carries the peer summary and `last_message_at` so the recipient's client can render the sidebar entry without a `GET /api/dms` round-trip.
- `read` frames are emitted to the originating viewer's `user:<viewer>` topic only (cross-device sync; no peer fan-out per L10).

`presence` frames are global join/leave deltas; clients seed the full set from `GET /api/presence` on (re)connect.

`channel` frames are global broadcasts emitted after a successful `POST /api/channels` or `PATCH /api/channels/{id}`; every connected client receives them regardless of which channel their WS connection is scoped to (channel listings live outside the per-WS channel scope). Inbound channel frames remain forbidden — the sender-spoofing rule from §"Design deviations" is unchanged; clients never write channel events into the WS.

### Direct messages (Phase 9)

```
POST /api/dms                   { "peer_user_id" }    → 201 on create / 200 on existing,
                                                        body: <Conversation>
GET  /api/dms                   (Bearer)              → { "conversations": [ <Conversation> ] }
                                                        # only conversations with at least one message
POST /api/dms/{id}/messages     { "body" }            → <DMMessage>   # 201; 404 on non-participation
GET  /api/dms/{id}/messages?limit=50&before=<msg_id>
                                                      → { "messages": [ <DMMessage> ] }
```

`POST /api/dms` is idempotent — given a peer, it find-or-creates the conversation in a single transaction (canonical pair ordering `user_a_id < user_b_id`; locked-in default L2). 201 is returned when the row was inserted, 200 when it already existed (L18). Self-DM (`peer_user_id == viewer`) returns 400.

`POST /api/dms/{id}/messages` shares its `MaxMessageBodyBytes = 4096` cap with channel messages (L16). Non-participation in the conversation returns 404, matching the channel-membership pattern (L8). The handler runs `InsertDMMessageTx`, which atomically inserts the message, advances `dm_conversations.last_message_id`/`last_message_at`, and materializes (or advance-only-updates) the sender's `dm_reads` row to the new message id (§11 / L21). Successful sends emit a `{type:"dm"}` WS frame to both participants' `user:<viewer>` topics.

`GET /api/dms` lists every conversation in which the viewer participates AND that has at least one message (decision §3 — empty conversations are hidden from the sidebar). The listing is ordered by `last_message_at DESC` and carries no body preview (§9). Each row includes the peer's `{id, username}` summary so the client can render the sidebar without a second `/api/users` lookup. No pagination in v1 (L12).

A new shared rate-limit bucket — `dm-write` (burst 10 / refill 1m) — gates `POST /api/dms/{id}/messages` per user. The shape mirrors the channel-write bucket; both are configured in `apps/server/internal/ratelimit/config.go` (L17).

### Read state (Phase 9)

```
POST /api/channels/{id}/read    { "message_id" }     → { "ok": true }
POST /api/dms/{id}/read         { "message_id" }     → { "ok": true }
```

Read state is server-authoritative and tracked per `(viewer, target)` pair so a viewer's "unread" badge survives across devices and reconnects (§5, §7). The `message_id` body field is required (L5); the handler refuses to advance to a message id less than the existing `last_read_*` (advance-only) and refuses to advance past the conversation's current `last_message_id`. A new shared rate-limit bucket — `read-mark` (burst 50 / refill 1m) — gates both endpoints per user (L17).

Channel listings (`GET /api/channels`) and DM listings (`GET /api/dms`) carry an additive `unread_count` field so a fresh login can render badges without N round-trips. The asymmetric initialization rules (auto-materialize for channels, lazy-NULL for DMs) are documented in §11 and `specs/plans/phase-9/read-state.md`.

WebSocket emits `{type:"read"}` frames so a viewer's other devices update their badge in real time (§7). The frame is fanned out only to the originating viewer's `user:<viewer>` topic; peers do not see read receipts (L10).

### Channel membership (Phase 10)

```
POST   /api/channels/{id}/members                                { "user_id", "membership": <MembershipBlock>, "root_key_wrap": <Wrap> }
                                                                 → 201; 400; 403 non-member; 404; 409 already-member; 429
GET    /api/channels/{id}/members                                → { "members": [ { "user_id", "username", "box_pubkey", "sign_pubkey", "added_at", "membership": <MembershipBlock> } ] }   # 200; 403 non-member
DELETE /api/channels/{id}/members/{user_id}                      → 204; 403 on `#general` (L8) or non-member-non-self; 404
```

Phase 10 introduces the explicit `channel_members` relation (decision-log §6 + §10): every membership-add carries an Ed25519 inviter-signature (`MembershipBlock` shape per `specs/plans/phase-10/encryption.md`) so a rogue server cannot inject itself into a private channel. The same call carries the wrap of the channel's current root key for the new member (atomic insert; L7 invariant). `#general` membership is immutable (L8) — `DELETE` returns 403. `DELETE` triggers a `members_changed` WS broadcast and a key rotation (L16 — sole rotation trigger in v1) per `specs/plans/phase-10/keys.md`. See `specs/plans/phase-10/membership.md`.

### Channel keys (Phase 10)

```
POST /api/channels/{id}/keys                                     { "generation_id", "wraps": [<WrapEntry>, ...] }
                                                                 → 201; 400 invalid_generation; 403 non-member; 409 race-loss; 429 wraps-needed bucket
GET  /api/channels/{id}/members/wraps-needed                     → { "channel_id", "is_public", "missing": [ { "user_id", "generation_id", "membership": <MembershipBlock> } ] }
                                                                 → 200; 403 non-member; 429 wraps-needed-read bucket
POST /api/channels/{id}/members/{user_id}/replay-wrap            { "membership": <MembershipBlock>, "root_key_wrap": <Wrap> }
                                                                 → 200; 400; 403 (caller is recipient OR not a member OR cool-down active); 429 replay-wrap bucket
```

The standalone keys-RPC has three modes distinguished by `generation_id`'s relationship to `max(channel_keys.generation_id)`: bootstrap (`max IS NULL`, single wrap-to-self), fill-in (`generation_id == max`, single wrap to a missing member), rotation (`generation_id == max + 1`, full wrap-list). `wraps-needed` carries the `MembershipBlock` per row so the verifier-side flow (L22) checks the inviter-signature chain BEFORE computing a wrap. `replay-wrap` is the wrap-integrity recovery path (L29 + L35) — rate-limited per `(channel_id, member_user_id)` pair with a 24-hour cool-down after 3 failures in 1 hour. New rate-limit buckets `wraps-needed-read` (burst 10 / refill 1m, L36) and `replay-wrap` (burst 3 / refill 5m / per-pair, L35) live in `apps/server/internal/ratelimit/config.go`. See `specs/plans/phase-10/keys.md`.

### Design deviations from earlier PRD revisions

Phase 2 implementation locked in several divergences from the original spec. Each was reviewed and resolved as a PRD update rather than a code change; this section is the canonical record so the spec stays honest without rewriting history.

| Area | Original spec | Implementation | Why kept | Locked in by |
|---|---|---|---|---|
| Web state + styling | Zustand + Tailwind | React Context + plain CSS (`apps/web/src/styles.css`) | Neither is load-bearing at 4-route scale; Context covers cross-component reads cleanly, plain CSS avoids a build-config and design-system commitment for an MVP. Revisit if either grows in. | PR #84 (web app), #105 (presence list) |
| WS inbound protocol | `{type:"subscribe"\|"unsubscribe"\|"send"}` typed frames | `?channel=<id>` at upgrade; sends via REST `POST /api/channels/{id}/messages`; no application-level inbound frames | Strictly safer — eliminates the client-supplied `channel_id` on send that allowed sender-spoofing (sec finding #3, fix `92d447f`). Cheaper too: one channel per WS removes hub fan-out branching. | commit `92d447f`, PR #88 |
| Presence frame shape | Per-channel snapshot `{channel_id, users:[{id,username}]}` on every change | Global delta `{kind:"join"\|"leave", user_id}` on transition + `GET /api/presence` snapshot for reconciliation | Cheaper on the wire (delta vs. full set), matches the implemented `usePresence` hook and the e2e suite. Snapshot on (re)connect closes the catch-up gap. | PR #80, #105 |
| CLI framework | cobra | stdlib `flag` + `splitFlagsAndPositional` helper (allows flags after positional args) | 9 flat commands don't justify cobra's footprint. Migration becomes worthwhile once subcommand groups, generated `--help`, or shell completion are needed (see §13). | PR #88, PR #117 |
| HTTP routing | `github.com/go-chi/chi/v5` | stdlib `net/http.ServeMux` with Go 1.22+ method+pattern syntax (`apps/server/internal/wiring/auth.go`, `apps/server/internal/wiring/presence.go`, `apps/server/internal/http/channels_handlers.go`) | Go 1.22+ pattern syntax covers our route count with zero added dependency; chi was never imported. Revisit if we need middleware composition or sub-routers chi handles natively. | issue #718 |
| List-endpoint payload shape | Bare arrays — `GET /api/channels → [...]`, `GET /api/channels/{id}/messages → [...]` | Wrapped under a named key — `{ "channels": [...] }` and `{ "messages": [...] }` (`apps/server/internal/http/channels_handlers.go:55`, `messages_handlers.go:90`) | Internal consistency with `GET /api/presence` (`{ "users": [...] }`), which the PRD already documents wrapped — one rule for all list endpoints. Forward-compat: the messages endpoint is already cursor-paged via `?before=`, and a wrapped object absorbs future paging metadata (`next_before`, `has_more`) without a breaking wire change. Both clients (`packages/api-client/src/http.ts`, `packages/go-client/channels.go`, `messages.go`) already consume the wrapped shape. | issue #713 |

## 11. Success Criteria

**MVP success** = all 10 user stories pass; a friend on a clean machine can clone, build, log in via CLI or Web, and exchange messages in real time across both clients with messages persisted across restarts.

### Functional checks (each maps to a test or scripted demo step)

| ID | Check |
|---|---|
| US-1 | Register creates a user, returns a valid token |
| US-2 | Login with correct credentials returns a token; wrong creds return generic error |
| US-3 | `GET /api/channels` returns at least the seeded `#general` |
| US-4 | `POST /api/channels` creates and lists; web Playwright covers the create-channel UI happy path; CLI `chatd channels create <name>` happy path round-trips through `GET /api/channels` |
| US-13 | `PATCH /api/channels/{id}` happy path renames an existing channel; rename of `#general` returns 403; rename to a name already in use returns 409; the `CHAT_CHANNEL_WRITE_BURST + 1`-th channel write inside one `CHAT_CHANNEL_WRITE_REFILL` window from one user returns 429; web Playwright covers the rename-channel UI happy path; CLI `chatd channels rename <current> <new>` happy path round-trips through `GET /api/channels` |
| US-5 | A message sent on one WS connection arrives on a second connection subscribed to the same channel within 500 ms |
| US-6 | History endpoint returns messages in created-at order with paging |
| US-7 | Connecting and disconnecting a client updates `presence` events within 2 s |
| US-8 | CLI `send` followed by CLI `watch` round-trips a message (smoke script) |
| US-9 | Web manual demo: login, send, receive across two browser windows |
| US-10 | Server boots from a clean directory with only env vars set |
| US-11 | Register without (or with wrong) `invite_code` returns generic auth error; with correct code, succeeds |
| US-12 | After `POST /api/auth/logout`, the previously issued JWT is rejected on `/api/auth/me` and `/api/auth/ws-ticket` |

### Security checks (must pass before MVP ships)

| Check | How |
|---|---|
| SEC-1 | Server refuses to start with missing/short/denylisted `CHAT_JWT_SECRET` |
| SEC-2 | Server refuses non-loopback bind unless `CHAT_ALLOW_PUBLIC_BIND=1` |
| SEC-3 | Login response time for unknown user is within 20% of wrong-password time (constant-time check) |
| SEC-4 | Login error message is byte-identical for unknown-user and wrong-password |
| SEC-5 | 11th login attempt within 5 min from one IP is rejected with 429 |
| SEC-6 | WS frame > 64 KiB closes the connection with `1009` |
| SEC-7 | REST body > 16 KiB returns 413 |
| SEC-8 | Message body > 4 KiB returns 400 on REST and `error` frame on WS |
| SEC-9 | Stored XSS attempt (`<script>` and `<img onerror>`) renders as text in web client |
| SEC-10 | Response headers include CSP, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options` |
| SEC-11 | Access logs of a login + WS upgrade contain no `token` or `ticket` value |
| SEC-12 | WS `?ticket=` rejected on second use within TTL |
| SEC-13 | `auth_events` table records register, login success/fail, logout for a scripted run |
| SEC-14 | SQLite file is mode `0600` after first boot |
| SEC-15 | `chatd logout` then reusing the cached token returns 401 (covers US-12) |

### Quality gates

- `go test ./...` green across all Go modules.
- `pnpm -r test` green across TS packages.
- `scripts/smoke.sh` passes.
- Manual cross-client demo: CLI ↔ Web round-trip.
- One-command bring-up: `pnpm dev`.
- README documents quick start.

### Schema additions (Phase 9)

Phase 9 adds four tables in a single migration `migrations/0005_dms_and_read_state.sql` (locked-in default L23). Every column listed below is part of the contract; new columns require a fresh migration plus a wire-types coordinated update (CLAUDE.md "Wire types").

`dm_conversations` — one row per ordered pair of users with at least one shared message attempt:

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT (ULID) | primary key (L1) |
| `user_a_id` | TEXT (ULID) | NOT NULL; canonical pair ordering `user_a_id < user_b_id` (L2) |
| `user_b_id` | TEXT (ULID) | NOT NULL; UNIQUE(`user_a_id`, `user_b_id`) |
| `last_message_id` | TEXT NULLABLE | denormalized for the listing query (L11); NULL until first message |
| `last_message_at` | TIMESTAMP NULLABLE | denormalized for the listing query (L11); NULL until first message |
| `created_at` | TIMESTAMP | NOT NULL |

Indexes: `(user_a_id)`, `(user_b_id)` for the listing query (L13).

`dm_messages` — one row per DM (immutable per L9):

| Column | Type | Notes |
|---|---|---|
| `id` | TEXT (ULID) | primary key |
| `conversation_id` | TEXT (ULID) | NOT NULL; FK → `dm_conversations.id` |
| `sender_user_id` | TEXT (ULID) | NOT NULL |
| `body` | TEXT | NOT NULL; cap = `MaxMessageBodyBytes = 4096` (L16) |
| `created_at` | TIMESTAMP | NOT NULL |

Indexes: `(conversation_id, id)` for paginated history (`?before=` cursor reuses the channel-message ULID-cursor pattern).

`channel_reads` — viewer-per-channel read pointer (auto-materialized by `GET /api/channels`):

| Column | Type | Notes |
|---|---|---|
| `channel_id` | TEXT (ULID) | NOT NULL; PK part 1 |
| `user_id` | TEXT (ULID) | NOT NULL; PK part 2 |
| `last_read_message_id` | TEXT (ULID) | NOT NULL; pinned at first-list to `channels.last_message_id` (§11 / decision-log §11) |
| `updated_at` | TIMESTAMP | NOT NULL |

Auto-materialization rule: every `GET /api/channels` request runs `INSERT OR IGNORE` for any (channel, viewer) row missing under the same transaction as the listing query, so a brand-new user sees `0 unread` on existing-history channels (rationale: every authenticated user is a member of every channel — PRD §9 — and the alternative behavior would surface "50K unread" on signup).

`dm_reads` — viewer-per-DM read pointer (lazy NULL):

| Column | Type | Notes |
|---|---|---|
| `conversation_id` | TEXT (ULID) | NOT NULL; PK part 1; FK → `dm_conversations.id` |
| `user_id` | TEXT (ULID) | NOT NULL; PK part 2 |
| `last_read_dm_message_id` | TEXT (ULID) NULLABLE | NULL means "viewer has never explicitly read" (treated as "all peer messages unread") |
| `updated_at` | TIMESTAMP | NOT NULL |

Asymmetric initialization: `GET /api/dms` does NOT auto-materialize `dm_reads` rows (decision-log §11). The sender's row is materialized inside `InsertDMMessageTx` (advance-only on subsequent sends — L21). The recipient's row is created only by the explicit `POST /api/dms/{id}/read` call (after the user has seen the messages). Until then NULL → all peer messages unread → correct badge for offline-arrived DMs.

Channel listings additively gain `unread_count` (and the embedded `last_message_id`/`last_message_at` denormalized columns become wire-visible). DM listings carry `unread_count` and the peer summary alongside the conversation row. Wire shapes for both are spelled out in `specs/plans/phase-9/dms.md` and `specs/plans/phase-9/read-state.md`.

### Schema additions (Phase 10)

Phase 10 adds three tables (`channel_members`, `channel_keys`, `dm_conversation_keys`) and rewrites the `messages` + `dm_messages` body columns into a seven-column signed envelope, all in a single migration `migrations/0006_encryption.sql` (locked-in default L32 — splitting risks merge conflicts on the next-sequence-number across parallel sub-issues). The migration also adds `users.box_pubkey` + `users.sign_pubkey`, `channels.is_public`, and the L37 `^[a-z0-9_-]{3,32}$` username regex tightening + `COLLATE NOCASE`. Per memory `project_no_production_deployment.md` the migration is destructive (L18 wipe-and-reset with a boot guard that aborts startup on a pre-encryption DB carrying rows). Wire shapes mirrored under CLAUDE.md "Wire types".

`users` — Phase 10 adds two columns:

| Column | Type | Notes |
|---|---|---|
| `box_pubkey` | BLOB (32 bytes) | NOT NULL; per-user X25519 wrap key (decision-log §3 + §4); base64 on the wire |
| `sign_pubkey` | BLOB (32 bytes) | NOT NULL; per-user Ed25519 signing key; base64 on the wire |

The login response, `GET /api/auth/me`, and the `User` wire type all return both pubkeys for client-side seed validation (L2). Private key material exists only on devices and in the user's password-manager vault — server stores zero recoverable secrets.

`channels` — Phase 10 adds one column:

| Column | Type | Notes |
|---|---|---|
| `is_public` | BOOLEAN | NOT NULL DEFAULT FALSE; `#general` seeded as `is_public = TRUE`; user-created channels default to private; immutable after creation (L15) |

`channel_members` — explicit membership relation (decision-log §6 + §10):

| Column | Type | Notes |
|---|---|---|
| `channel_id` | TEXT (ULID) | NOT NULL; PK part 1; FK → `channels.id` |
| `user_id` | TEXT (ULID) | NOT NULL; PK part 2; FK → `users.id` |
| `inviter_user_id` | TEXT (ULID) | NOT NULL; FK → `users.id`; the user who signed this row |
| `inviter_sign_pubkey` | BLOB (32 bytes) | NOT NULL; pinned at invite time so inviter rotation does not invalidate the stored signature (L34 history-vs-current rule) |
| `inviter_signature` | BLOB (64 bytes) NULLABLE | Ed25519; NULL is permitted ONLY for public-channel server-auto-add (R1.2 carve-out); L33 enforces NOT NULL at the application layer for any row whose channel has `is_public = FALSE` |
| `invitee_box_pubkey` | BLOB (32 bytes) | NOT NULL; pinned at invite time (frozen to what the inviter SIGNED, not looked up live) |
| `invitee_sign_pubkey` | BLOB (32 bytes) | NOT NULL; pinned at invite time |
| `added_at` | TIMESTAMP | NOT NULL |

Plus a non-unique partial index `idx_channel_members_null_sig (channel_id, user_id) WHERE inviter_signature IS NULL` for forensic scans of operator-injected NULL-sig rows (L33). The signature scope is `b"snakd-mship-v1:" || channel_id || b"|" || user_id || b"|" || inviter_user_id || b"|" || inviter_sign_pubkey || b"|" || invitee_box_pubkey || b"|" || invitee_sign_pubkey || b"|" || added_at_rfc3339` per `specs/plans/phase-10/encryption.md`.

`channel_keys` — per-recipient root-key wraps for channels:

| Column | Type | Notes |
|---|---|---|
| `channel_id` | TEXT (ULID) | NOT NULL; PK part 1; FK → `channels.id` |
| `generation_id` | INTEGER | NOT NULL; PK part 2; per-channel monotonic; bumps on member-removal rotation only (L16) |
| `member_user_id` | TEXT (ULID) | NOT NULL; PK part 3; FK → `users.id` |
| `wrapped_key` | BLOB (48 bytes) | NOT NULL; `crypto_box` ciphertext (32 payload + 16 Poly1305 MAC) of the channel root key, encrypted to `member_user_id`'s `box_pubkey` |
| `sender_box_pubkey` | BLOB (32 bytes) | NOT NULL; the wrapper's `box_pubkey` at wrap time (server-validated against caller per L30) |
| `nonce` | BLOB (24 bytes) | NOT NULL; XSalsa20 random nonce |
| `created_at` | TIMESTAMP | NOT NULL |

`dm_conversation_keys` — per-recipient wraps for DM root keys (no `generation_id` column — DMs never rotate, L6):

| Column | Type | Notes |
|---|---|---|
| `conversation_id` | TEXT (ULID) | NOT NULL; PK part 1; FK → `dm_conversations.id` |
| `member_user_id` | TEXT (ULID) | NOT NULL; PK part 2; FK → `users.id` |
| `wrapped_key` | BLOB (48 bytes) | NOT NULL; same shape as `channel_keys.wrapped_key` |
| `sender_box_pubkey` | BLOB (32 bytes) | NOT NULL |
| `nonce` | BLOB (24 bytes) | NOT NULL |
| `created_at` | TIMESTAMP | NOT NULL |

`messages` — the `body TEXT NOT NULL` column is dropped and replaced by seven envelope columns (L23):

| Column | Type | Notes |
|---|---|---|
| `cipher_suite` | INTEGER | NOT NULL; `0x01 = naclbox-v1` in v1; future suites bump and add explicit support |
| `key_generation_id` | INTEGER | NOT NULL; references `channel_keys.generation_id` |
| `nonce` | BLOB (24 bytes) | NOT NULL; XSalsa20 random |
| `ciphertext` | BLOB | NOT NULL; `crypto_secretbox(plaintext, nonce, channel_root_key)` |
| `sender_sign_pubkey` | BLOB (32 bytes) | NOT NULL; signer's CURRENT `sign_pubkey` at send time |
| `signature` | BLOB (64 bytes) | NOT NULL; Ed25519 over the `snakd-msg-v1:channel:` scope (L21) |
| `client_created_at` | TIMESTAMP | NOT NULL; client-stamped, signed (rogue server cannot forge ordering) |

The unsigned, server-stamped `created_at` column is preserved for delivery-time display. Existing `messages.user_id` column stays (renaming would balloon the diff per L23 even though wire-side naming is `sender_user_id`).

`dm_messages` — same envelope rewrite as `messages`. The `body TEXT NOT NULL` column is dropped; the same seven envelope columns are added; `signature` covers the `snakd-msg-v1:dm:` scope (cross-protocol-confusion defense — channel and DM ciphertexts cannot be replayed across boundaries even if a malicious server swaps the wire shape).

### UX

- Web does not block on send (optimistic render).
- Web auto-reconnects within 5 s after a server restart.
- Errors are user-friendly (no stack traces in client UI).

## 12. Implementation Phases

Total budget: ~8 hours.

### Phase 0 — Walking skeleton, system test ready (~1 hr)

**Goal**: server up, two CLI clients exchanging real-time messages over WebSocket. No auth, no DB, hardcoded `#general`. Prove the wire end-to-end.

Deliverables:
- Monorepo scaffold: `pnpm-workspace.yaml`, root `package.json` with `dev` / `build` / `test` scripts, single root `go.mod` with module name `hackathon`.
- `apps/server`: `/ws` endpoint with in-memory hub, broadcasts every received message to all subscribers of the channel.
- `apps/cli`: `chatd send` and `chatd watch` against `/ws` (no login).
- **System test**: `scripts/smoke.sh` boots server, runs two `chatd watch` processes, pipes a message via `chatd send`, asserts both watchers see it.

**Validation**: `scripts/smoke.sh` passes. This stays green for the rest of the project.

### Phase 1 — Persistence + auth (~2 hrs)

**Goal**: real users, channels, messages persisted to SQLite.

Deliverables:
- SQLite schema (`migrations/0001_init.sql`) including `users.token_version` and `auth_events` table.
- ULID generation.
- `internal/auth`: bcrypt + JWT with `tv` claim; constant-time login path; password length policy.
- Auth endpoints: register (invite-code gated) / login / me / logout / ws-ticket.
- Channels endpoints; messages endpoints (REST + WS).
- Hardening that must land in Phase 1 (not Phase 3):
  - Startup checks: JWT secret length + dev-default denylist; non-loopback bind requires `CHAT_ALLOW_PUBLIC_BIND=1`; registration requires `CHAT_INVITE_CODE`.
  - Per-IP rate limits on login and registration; per-username login backoff.
  - WS read limit (64 KiB), per-conn send rate limit, 4 KiB body cap, REST 16 KiB body cap.
  - Same-origin WS upgrade check; one-shot 30s ws-ticket flow; WS rejects sends to non-existent channels.
  - Access-log middleware strips `token` and `ticket` query params; user-safe error envelope.
  - SQLite file created `0600`.
  - Response security headers (CSP, nosniff, no-referrer, frame-deny).
- Tests for US-1, US-2, US-3, US-4, US-5, US-6, US-11, US-12 and SEC-1…SEC-15.

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

### Shipped post-MVP

- **Direct messages, 1:1 only** — shipped Phase 9. See `specs/plans/phase-9/dms.md` for wire-type definitions and the `{type:"dm"}` envelope.
- **Server-tracked read state for channels and DMs** — shipped Phase 9. See `specs/plans/phase-9/read-state.md` for `channel_reads` / `dm_reads` schema and the `{type:"read"}` envelope.
- **End-to-end encryption (channels + DMs)** — shipped Phase 10. NaCl `crypto_box` / `crypto_secretbox` / `crypto_sign` primitives; per-user `(box_pubkey, sign_pubkey)` derived from passphrase via Argon2id + HKDF-SHA256; static root key per channel/DM, wrapped per recipient via `crypto_box`; signed envelope (`cipher_suite`, `key_generation_id`, `nonce`, `ciphertext`, `sender_sign_pubkey`, `signature`, `client_created_at`) replaces the plaintext `body` column. See `specs/plans/phase-10/encryption.md` (wire types, signature scopes, KDF parameters), `specs/plans/phase-10/membership.md` (channel-membership relation, inviter-signed rows, `#general` auto-add), `specs/plans/phase-10/keys.md` (`channel_keys` / `dm_conversation_keys` schema, three modes of `POST /api/channels/{id}/keys`, lazy-wrap-on-online flow), and `specs/plans/phase-10/security.md` (threat model option C, R1.1 modified-client residual, R1.2 operator-as-public-channel-member residual, accepted metadata exposure).
- **Public-vs-private channels with explicit membership** — shipped Phase 10. `channels.is_public BOOLEAN DEFAULT FALSE`; new `channel_members` table with inviter-signed rows; `#general` is the seeded `is_public = TRUE` baseline; user-created channels default to private. See `specs/plans/phase-10/membership.md`.

### Roadmap
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
- **Web state lib + design system** — adopt Zustand (or similar) when cross-route state mutation outgrows Context, and Tailwind (or similar) when the design surface outgrows hand-rolled CSS.
- **CLI framework migration** — adopt cobra/pflag once subcommand groups, generated `--help`, or shell completion are needed.

## 14. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Two clients in one day is tight | Medium | High | Phase 0 walking skeleton de-risks E2E early; shared `go-client` and `api-client` remove duplication; CLI is far smaller than Web and finishes first |
| Web UI bundling drags Phase 0 timing | Low | Medium | Phase 0 has no web; web work is Phase 2 |
| JWT secret leakage | Low | High | Required env var; server refuses to start without ≥32-byte secret; secret is never logged |
| WebSocket hub deadlocks / leaks | Medium | High | Per-connection bounded write channel; close on slow consumer; tests assert subscribe/unsubscribe count under churn |
| Scope creep into post-MVP features | High | High | This PRD is the contract. Anything in §13 stays out until §11 ships. |
| SQLite write-lock contention under multiple writers | Low | Low | Single-process server with serialized writes; friend-scale traffic is far below SQLite's threshold |
| Accidental public exposure of an unhardened instance | Medium | Critical | Loopback bind by default; non-loopback requires `CHAT_ALLOW_PUBLIC_BIND=1`; registration requires `CHAT_INVITE_CODE`; same-origin WS check |
| Stored XSS in message body defaces all clients | Medium | High | Plain-text rendering only; no `dangerouslySetInnerHTML`; strict CSP; no markdown/auto-link in MVP |
| Stolen JWT usable for 7 days | Medium | High | `token_version` claim; logout and password change bump it; all prior tokens reject |
| JWT leaks via URL (logs / history / Referer) | Medium | Medium | WS uses one-shot 30s ticket, not the session JWT; access-log middleware strips `token`/`ticket` query params |
| Account enumeration via login timing or error text | Medium | Medium | Constant-time bcrypt against dummy hash on unknown user; identical error message |
| Brute-force login or registration spam | Medium | Medium | Per-IP rate limits on both; per-username linear backoff on login |
| WS abuse: oversize frames, flooding, channel-ID spoofing | Medium | High | 64 KiB read limit; 10 msg/s send limit; channel existence check; 4 KiB body cap |

## 15. Appendix

### Related documents
- This PRD: `specs/PRD.md`
- Changelog: `CHANGELOG.md`
- README (Phase 3 deliverable): `README.md`
- Build process: features are implemented by Claude Code agent teams (one team per feature, members `impl` / `bull` / `qual`).

### Key dependency links
- pnpm: https://pnpm.io
- pnpm workspaces: https://pnpm.io/workspaces
- nhooyr.io/websocket: https://pkg.go.dev/nhooyr.io/websocket
- modernc.org/sqlite: https://pkg.go.dev/modernc.org/sqlite
- goose: https://github.com/pressly/goose
- React: https://react.dev
- Vite: https://vite.dev
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
8. E2E encryption (channels + DMs) shipped Phase 10 — see §13 "Shipped post-MVP" and `specs/plans/phase-10/`. Encryption at rest, federation, and Postgres are explicitly **not** prepared for in code. They will require schema and code changes when they ship — that is the right time to design them.
