# Feature: Single-binary demo path verified

**Parent phase:** [Phase 3: Polish, demo](../phase-3-polish-demo.md)
**Status:** planned

## Requirements covered
- US-10 — As the host, I want a single binary with env-var config, so deploying for friends is trivial.

## Acceptance criteria
- A single Go binary, configured solely via env vars, serves both the API/WS and the embedded web app.
- All three required env vars must be set: `CHAT_JWT_SECRET`, `CHAT_INVITE_CODE`, and `CHAT_DB_PATH`. None has a default; the server fails before opening a port if any is missing. `CHAT_LISTEN_ADDR` defaults to `127.0.0.1:8080`.
- A documented manual demo path: build the binary → set the three required env vars → run → register via web → send a message → see it in CLI watch.
- The Phase 3 validation criterion is met: clean clone → `pnpm dev` → full demo flow under 5 minutes.

## Implementation steps
1. Confirm the config loader (`feature-startup-config-checks.md`, `apps/server/internal/config/config.go`) recognises every env var the demo touches. Note: `CHAT_LISTEN_ADDR` (not `CHAT_BIND`) defaults to loopback; `CHAT_DB_PATH` has no default and must be set explicitly for the demo.
2. Add a `pnpm demo` script in root `package.json` (and/or a `Makefile` target) that builds web + server, then runs the resulting binary with a known invite code, a strong (32+ byte) JWT secret, and `CHAT_DB_PATH` pointing at a temp file. Neither target exists today.
3. Record/document the single-binary demo path in the README.
4. Walk through the path manually, fixing any rough edges encountered.

## Test plan
- Manual: build the single binary, run with only `JWT_SECRET` and `CHAT_INVITE_CODE` set, complete a register → send → receive flow in the browser.
- Manual: confirm the CLI also works against the same binary using the configured server URL.
- `test_binary_starts_with_minimal_env` — covers US-10 minimum-config startup. Lives at `tests/e2e/phase-3/single-binary-demo/demo_test.go`; runs `scripts/build-single-binary.sh` end-to-end, boots the produced binary with the auth-enabled env vars, and asserts both arms (HTTP `/` serves the real Vite SPA — proven by a `/assets/*.js` reference the placeholder lacks; `/api/auth/register` returns the PRD §10 JSON envelope, not the SPA HTML). Skips when `pnpm` is not on `PATH` so CI's `go` job (no pnpm setup) stays green; the `pnpm` and full-`e2e` jobs install pnpm and run it for real.

## Verified demo path

Reproduce locally:

```bash
pnpm --filter web build
bash scripts/build-single-binary.sh   # writes ./bin/chat-server with the SPA embedded
CHAT_JWT_SECRET="$(openssl rand -hex 32)" \
CHAT_INVITE_CODE="$(openssl rand -hex 8)" \
CHAT_DB_PATH="$(mktemp -t chatd.XXXXXX.sqlite)" \
  ./bin/chat-server
# then open http://127.0.0.1:8080
```

`scripts/build-single-binary.sh` is the canonical orchestration: it runs `pnpm --filter web build`, copies `apps/web/dist/*` into `apps/server/internal/web/dist/` (whose contents are `.gitignore`d except the tracked placeholder), then `go build -o ./bin/chat-server ./apps/server`. `CHAT_LISTEN_ADDR` defaults to `127.0.0.1:8080`; override it (and set `CHAT_ALLOW_PUBLIC_BIND=1`) only when the demo is intentionally exposed off the loopback.

## Files expected to be touched or created
- `Makefile` or `package.json` (demo target)
- `README.md` (demo path section, coordinated with `10-feature-readme-quick-start.md`)
- `apps/server/internal/config/config.go` (defaults verified, no schema changes expected)

## Risks
- A subtle CSP/origin mismatch can break the embedded UI under the single-binary deploy; mitigated by exercising the demo path manually and resolving issues before declaring the feature done.
