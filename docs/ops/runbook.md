# Operations runbook

This is the end-to-end deploy story for a hackathon/homelab install: clone, configure, run, register the first user, take a backup, and put a reverse proxy in front. One file, top-to-bottom.

The compose file at the repo root (`docker-compose.yml`) drives a single `chat-server` service. There is no second container, no sidecar, no orchestrator.

## Prerequisites

- Docker Engine 24+ with Compose v2 (`docker compose version` should print `Docker Compose version v2.x`).
- `openssl` on the host for generating secrets.
- (Optional) `sqlite3` on the host for backup/restore. There are workarounds below if you don't have it.

A single Linux/macOS box is enough. There is no high-availability story; see the topology section.

## First run

```bash
git clone <this repo>
cd Hackathon

cp .env.example .env

# Generate fresh secrets and paste them into .env. The validator will
# refuse to start with the placeholder values shipped in .env.example.
openssl rand -hex 24    # CHAT_JWT_SECRET — needs >=32 ASCII bytes, >=5 distinct
openssl rand -hex 8     # CHAT_INVITE_CODE — any non-empty string

# Boot. --build forces a rebuild on every up; -d detaches.
docker compose up --build -d

# Tail logs until you see "chat server listening".
docker compose logs -f chat-server
```

Open `http://<host>:8080/` in a browser. The SPA loads from the embedded bundle inside the binary.

If the container exits immediately with `config: CHAT_JWT_SECRET ...`, the validator rejected the secret in `.env`. Edit `.env`, then `docker compose up -d` again (no `--build` needed for env-only changes — the env file is read at container start, not at build time).

## First-user registration

There is no admin role; the first registered user is just a regular user.

1. Visit `http://<host>:8080/`.
2. Click **Register**.
3. Pick a username and password.
4. Paste the value of `CHAT_INVITE_CODE` from your `.env`.
5. Submit.

Share the same `CHAT_INVITE_CODE` out-of-band with anyone you want to let in. To rotate it, edit `.env` and `docker compose restart chat-server`. Existing accounts continue to work; only new registrations need the new code.

## Daily ops

```bash
docker compose logs -f chat-server      # follow logs
docker compose logs --tail=100          # last 100 lines

docker compose restart chat-server      # restart in place
docker compose stop                     # stop, keep state
docker compose start                    # start back up

docker compose down                     # stop and remove containers; volume preserved
docker compose down -v                  # stop and DESTROY the named volume (chat-data)
```

`docker compose down` is safe — it preserves the `chat-data` volume, so message history survives. `docker compose down -v` is destructive; it deletes the SQLite database with the rest of the volume.

## Restart-loop recovery

If the container is restart-looping (you'll see it cycle in `docker compose ps` with repeated `Restarting` states), the cause is almost always a misconfigured `.env` that the startup validator rejects.

```bash
docker compose stop                     # halt the loop
docker compose logs --tail=100          # find the rejection line
# Typical lines:
#   config: CHAT_JWT_SECRET is too short: got 16 bytes, need at least 32
#   config: CHAT_JWT_SECRET matches a known dev-default value
#   config: CHAT_INVITE_CODE is required while registration is enabled
#   config: CHAT_LISTEN_ADDR="0.0.0.0:8080" is non-loopback; set CHAT_ALLOW_PUBLIC_BIND=1
# Edit .env to fix the rejection.
docker compose up -d
```

Stopping first matters: while the restart loop is active, you'll see one new error block roughly every few seconds, which makes log-tailing noisy. Stop, then read.

## Backup and restore

The SQLite file inside the volume is the entire persistent state. Back it up with `sqlite3 ".backup"`, not `cp`.

### Why not `cp`

`apps/server/internal/db/open.go:39` opens the database with `_pragma=journal_mode(WAL)`. WAL mode means writes go to `chat.db-wal` first and only periodically merge into `chat.db`. A naive `cp chat.db chat.db.bak` while the server is running can capture an inconsistent snapshot — half the recent writes are still in the `-wal` sidecar. `sqlite3 ".backup"` is the safe online-backup API; it copies a consistent snapshot whether or not writes are in flight.

### Distroless gotcha

The production image is `gcr.io/distroless/static-debian12:nonroot`. It contains exactly one binary (`/chat-server`) and nothing else — no `sh`, no `sqlite3`, no `cp`. Do not try:

```bash
# WILL NOT WORK:
docker compose exec chat-server sqlite3 /data/chat.db ".backup /data/chat.bak"
```

You have two working options instead.

### Recommended (host has `sqlite3`)

```bash
docker compose stop chat-server
sqlite3 ./data/chat.db ".backup ./data/chat.bak"
docker compose start chat-server
```

This assumes you exposed the named volume's path on the host. If you used the default named volume from the compose file (no host bind-mount), see the alternative below.

### Alternative (host lacks `sqlite3`, or volume is not bind-mounted)

Run a throwaway container that mounts the same named volume and has `sqlite3` available:

```bash
docker compose stop chat-server
docker run --rm -v chat-data:/data alpine/sqlite \
  sqlite3 /data/chat.db ".backup /data/chat.bak"
docker compose start chat-server
```

The backup file is now at `/data/chat.bak` inside the volume. Copy it out with another throwaway container:

```bash
docker run --rm -v chat-data:/data -v "$PWD":/out alpine \
  cp /data/chat.bak /out/chat.bak
```

You now have `./chat.bak` on the host.

### Restore

Restoring requires the server to be stopped. The WAL sidecar files (`chat.db-wal`, `chat.db-shm`) reflect the _old_ database; leaving them next to a freshly-restored `chat.db` will silently corrupt the result on next startup.

```bash
docker compose stop chat-server

# Replace chat.db inside the volume and remove stale -wal/-shm sidecars.
docker run --rm -v chat-data:/data -v "$PWD":/in alpine sh -c '
  rm -f /data/chat.db /data/chat.db-wal /data/chat.db-shm &&
  cp /in/chat.bak /data/chat.db
'

docker compose start chat-server
docker compose logs -f chat-server   # confirm "chat server listening" + "db ready"
```

Restoring while the server is running is unsupported; the live writer will overwrite your work, or the database will lock and the server will log errors.

## Reverse-proxy topology and single-instance constraint

### Single-instance constraint, plain words

**Run exactly one `chat-server` container.** The hub (`apps/server/internal/hub/hub.go`), ticket store, login rate limiter, and SQLite writer (`apps/server/internal/db/open.go:48` `SetMaxOpenConns(1)`) are all in-process. Two containers behind a load balancer would have two separate hubs and two separate rate-limit counters; messages sent through one would not appear on the other, and rate limits would not be enforced consistently. **Multi-instance deploy is not supported.**

If you outgrow a single instance, that's a different system from this one — it will need a real broker (NATS / Redis pub/sub) and a centralized rate-limit store.

### Recommended deploy

```
client  →  reverse proxy (TLS, gzip, X-Forwarded-For)  →  chat-server container  →  SQLite volume
```

TLS terminates at the proxy; the chat-server binary speaks plain HTTP and does not load certificates (PRD §347).

### Env vars to set when behind a proxy

These names map 1:1 to consts in `apps/server/internal/config/config.go` (lines 17–25) and `apps/server/main.go` (lines 21–31). The names below are the canonical strings; do not rename.

```env
CHAT_LISTEN_ADDR=0.0.0.0:8080      # bind all interfaces inside the container
CHAT_ALLOW_PUBLIC_BIND=1           # required when CHAT_LISTEN_ADDR is non-loopback
CHAT_TRUSTED_PROXY=1               # honor the leftmost X-Forwarded-For for source IP
CHAT_ALLOWED_ORIGINS=https://chat.example.com   # WS Origin allowlist
```

Without `CHAT_TRUSTED_PROXY=1`, every request looks like it came from the proxy's IP, so the per-IP login rate limiter and audit log all collapse onto a single key — the server emits a `WARN` at startup naming this exact failure mode.

`CHAT_LISTEN_ADDR` defaults to `127.0.0.1:8080` (loopback-only). Inside a container, that won't accept traffic from the proxy on a separate container or host network. Override to `0.0.0.0:8080` and pair it with `CHAT_ALLOW_PUBLIC_BIND=1` so the validator allows the non-loopback bind.

### Illustrative Caddyfile

This is a starting point, not a maintained config. Substitute your own hostname.

```caddy
chat.example.com {
  encode gzip
  reverse_proxy chat-server:8080 {
    header_up X-Forwarded-For {http.request.remote.host}
    header_up X-Forwarded-Proto {http.request.scheme}
  }
}
```

Caddy auto-provisions Let's Encrypt certs for `chat.example.com`, terminates TLS, and forwards plain HTTP plus `X-Forwarded-*` headers to the chat-server upstream. `nginx` and `traefik` work the same way; we don't ship configs for those.

### `docker inspect` env-var caveat

`CHAT_JWT_SECRET`, `CHAT_INVITE_CODE`, and any other secret in `.env` are visible to anyone with access to the Docker socket:

```bash
docker inspect chat-server | jq '.[0].Config.Env'
# ...
# "CHAT_JWT_SECRET=...the-real-secret..."
```

Treat docker-socket access as equivalent to "can read every secret on this host." Don't expose the socket to other containers casually, don't add users to the `docker` group casually, and don't share an SSH login that has socket access.

Docker Secrets, HashiCorp Vault, and similar external secret stores are out of scope for hackathon-grade deploys. If you need them, you've outgrown this setup.

## Upgrade

```bash
git pull
docker compose up --build -d
```

`--build` is required: the compose file uses `build: .`, so a code change without `--build` will keep running the old image. There is no separate `docker pull` step.

The named volume `chat-data` is not touched by a rebuild; the database survives across upgrades. Migrations run at boot from `migrations/` inside the new image.

## Rebuild trade-off

`build: .` rebuilds the image from local source on every `docker compose up --build`. Cost:

- **Pro:** no registry to set up, no image-tagging story, no auth flow for pulling. `git pull && docker compose up --build -d` is the entire upgrade procedure.
- **Con:** every code change adds ~30–60s to `up` while the multi-stage build runs (web bundle, Go static binary, distroless final stage). Pure config/env changes don't need `--build` and are fast.

For a hackathon or homelab single-host deploy, this is the right trade. If you want to push a registry image instead, swap `build: .` for `image: <your-registry>/chat-server:<tag>` and run a separate build/push step in CI; the rest of the compose file is unchanged.

## No compose healthcheck

The compose file deliberately does not declare a `healthcheck:` block. The production image is `gcr.io/distroless/static-debian12:nonroot` — no shell, no `wget`, no `curl`. A compose healthcheck of the form `CMD-SHELL ... wget ...` cannot run inside this image.

Liveness today is the reverse proxy's upstream health check hitting `/healthz` from outside the container. A self-contained `--health-probe` flag on the chat-server binary, which would let compose declare a `CMD ["/chat-server", "--health-probe"]` healthcheck without needing a shell or external tool, is filed as https://github.com/steen/Hackathon/issues/796.
