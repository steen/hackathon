### Added

- `CHAT_LOG_LEVEL` environment variable (PRD §9): one of `debug`, `info`, `warn`, `error` (default `info`). Server bootstrap now logs through `log/slog` with a leveled text handler; messages below the configured level are dropped. Unknown values fall back to `info` and the server logs a one-line warn naming the rejected value. Access-log middleware keeps its existing stdlib `log.Printf` output.
