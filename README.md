# Hackathon

A small chat app: a Go server (HTTP + WebSocket), a React/Vite web client, and a Go CLI (`chatd`). Built for the AI hackathon, May 2026.

- `apps/server` — HTTP/WS server, SQLite-backed when `CHAT_DB_PATH` is set.
- `apps/web` — React SPA, served via Vite in dev (proxies to the server).
- `apps/cli` — `chatd`, a terminal client for register/login/send/watch.

## Quick start (development)

Prerequisites: Go 1.22+, Node 20+, [pnpm](https://pnpm.io/), `openssl` (for generating a JWT secret).

Two terminals — one for the Go server, one for the web dev proxy.

```bash
# 1. Install JS deps (once per clone, and after any lockfile change).
pnpm install

# 2. Generate a JWT secret and pick an invite code.
#    openssl rand -hex 24 → 48 hex bytes (well over the 32-byte floor).
export CHAT_JWT_SECRET="$(openssl rand -hex 24)"
export CHAT_INVITE_CODE="$(openssl rand -hex 8)"
export CHAT_DB_PATH="$PWD/.dev.db"
```

Terminal 1 — start the server:

```bash
go run ./apps/server
# logs: "config check ok: ...", "chat server listening on 127.0.0.1:8080"
```

Terminal 2 — start the web dev server (Vite, port 5173, proxies `/api` and `/ws` to `127.0.0.1:8080`):

```bash
pnpm dev
```

Then:

1. Open <http://localhost:5173>.
2. Register a user with the invite code from `$CHAT_INVITE_CODE`.
3. Send a message in the `general` channel.

Target: clean clone to first message in under 5 minutes.

### CLI alternative

The CLI talks to the server directly (no web app needed):

```bash
go run ./apps/cli register <username>          # prompts for password and invite code
go run ./apps/cli login    --username <name>   # prompts for password
go run ./apps/cli watch    <channel-id>        # in one terminal
go run ./apps/cli send     <channel-id> "hi"   # in another
```

The CLI honours `$CHAT_SERVER` (default `http://localhost:8080`).

## Single-binary build

For demo deploys the web app is embedded into the server binary, so `go build ./apps/server` produces a single executable that serves both the API/WS and the SPA. See [`specs/plans/phase-3/40-feature-single-binary-demo-verified.md`](specs/plans/phase-3/40-feature-single-binary-demo-verified.md) for the full path; the embed wiring lives in [`specs/plans/phase-3/20-feature-embedded-web-build.md`](specs/plans/phase-3/20-feature-embedded-web-build.md).

## Server environment variables

The server reads the following at startup (`apps/server/internal/config/config.go` + `apps/server/main.go`). Validation runs once at boot; failures abort the process before any port is opened.

| Var | Required | Default | Purpose |
|-----|----------|---------|---------|
| `CHAT_JWT_SECRET` | yes | — | JWT signing key. ≥32 ASCII bytes, ≥5 distinct bytes, not a single repeated char, not on the dev-default denylist (`change-me`, `secret`, `password`, `dev`, `test`, `placeholder`, `hackathon`, …). Validated unconditionally at startup, even when `CHAT_DB_PATH` is unset. |
| `CHAT_INVITE_CODE` | yes | — | Gate code for registration. Any non-empty string. Validated unconditionally at startup. |
| `CHAT_DB_PATH` | for the auth/persistence boot path | — | SQLite file path. When set, the server mounts the auth + channels + messages handlers and boots the migration runner. When unset, the server runs in **phase-0 mode** with the WS hub and `/debug/subs` only (no auth, no SQLite); not used by `scripts/smoke.sh` (which sets a `$WORK_DIR`-scoped DB path) and not intended for real use. |
| `CHAT_LISTEN_ADDR` | no | `127.0.0.1:8080` | `host:port` to bind. Non-loopback hosts are rejected unless `CHAT_ALLOW_PUBLIC_BIND=1`. |
| `CHAT_ALLOW_PUBLIC_BIND` | no | unset | Set to `1` to allow a non-loopback bind (e.g. `0.0.0.0:8080`). The server logs a `WARN` because `CHAT_TRUSTED_PROXY` is not yet wired (PRD §9), so behind a proxy IP rate limits collapse onto the proxy IP. |
| `CHAT_ALLOWED_ORIGINS` | no | same-origin only | Comma-separated WebSocket `Origin` allowlist. Stray empty entries are dropped. |
| `CHAT_SERVER_PORT` | no | — | Legacy compatibility — replaces the port half of `CHAT_LISTEN_ADDR` without changing the host. |

### Client environment variable

| Var | Consumed by | Default | Purpose |
|-----|-------------|---------|---------|
| `CHAT_SERVER` | `apps/cli` (`chatd`) | `http://localhost:8080` | Base URL the CLI hits for HTTP and WebSocket. Not read by the server. |

## Troubleshooting

- **`CHAT_JWT_SECRET is too short` / `matches a known dev-default value`** — generate a fresh one: `openssl rand -hex 24`.
- **`CHAT_INVITE_CODE is required while registration is enabled`** — export `CHAT_INVITE_CODE` in the same shell that runs the server.
- **`CHAT_LISTEN_ADDR=… is non-loopback`** — keep the default, or set `CHAT_ALLOW_PUBLIC_BIND=1` if you actually want to expose the port.
- **Web app loads but registration fails with a 4xx** — the invite code in the browser must match `$CHAT_INVITE_CODE` in the server's shell.
- **Web app loads but WebSocket doesn't connect** — check the server is on `127.0.0.1:8080` (Vite proxies there); if you moved it, set `CHAT_ALLOWED_ORIGINS=http://localhost:5173` and adjust the Vite proxy.
- **`port 5173 is already in use` / `port 8080 is already in use`** — kill the stragglers, or set `CHAT_SERVER_PORT` (server) and rerun `pnpm dev` after editing `apps/web/vite.config.ts`'s proxy target.
- **Phase-0 mode (no auth, no DB)** — happens when `CHAT_DB_PATH` is unset. Register/login endpoints are not mounted; use it only for the smoke harness.

## Tests and CI mirror

```bash
go build ./... && go test ./...
bash scripts/smoke.sh
pnpm install --frozen-lockfile
pnpm -r --if-present build
pnpm -r --if-present test
pnpm run lint
pnpm run format:check
```

To fix formatting locally before CI, run `pnpm run format` (writes `prettier --write .` across the tree).

CI runs the same blocks (`.github/workflows/ci.yml`). Working agreements live in [`CLAUDE.md`](CLAUDE.md).
