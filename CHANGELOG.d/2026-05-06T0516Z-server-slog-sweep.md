### Changed

- Server-internal `log.Printf` callsites in `apps/server/internal/{wiring,wsapi,http}` now route through `slog`, so `CHAT_LOG_LEVEL` actually quiets these lines (previously they bypassed the leveled handler). The access-log middleware still writes via stdlib `log` to preserve the AC-1 raw-line wire contract.
