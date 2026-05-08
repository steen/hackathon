### Removed

- `CHAT_SERVER_PORT` env var. It only ever overrode the port half of `CHAT_LISTEN_ADDR`, which strictly supersedes it. Set `CHAT_LISTEN_ADDR=127.0.0.1:<port>` instead. `apps/server/listen_addr.go::resolveListenAddr` is gone; `parseAllowedOrigins` moved to `apps/server/origins.go`.
- The duplicate `<lowercase>Env` const block in `apps/server/main.go` (`portEnv`, `dbPathEnv`, `jwtSecretEnv`, `inviteCodeEnv`, `allowedOriginsEnv`). Bootstrap now reads the canonical `config.Env*` consts; `EnvDBPath` and `EnvAllowedOrigins` were added to `apps/server/internal/config/config.go` to fill the gap.
- `LEGACY_ENV_CONST_PATTERN` and the string/comment-aware Go const-block walker in `scripts/check-env-example.mjs`. The drift check now scans only `config.go`'s `Env*` consts. `tests/check-env-example/walker.test.mjs` is gone with the helpers it covered.

### Changed

- `scripts/smoke.sh` and every Go/TS test harness that previously injected a free port via `CHAT_SERVER_PORT` now uses `CHAT_LISTEN_ADDR=127.0.0.1:<port>`. `.env.example` and the `README.md` env-var table dropped the `CHAT_SERVER_PORT` row; the troubleshooting line for port conflicts now points at `CHAT_LISTEN_ADDR`.
