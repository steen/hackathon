### Added

- `packages/go-client`: `WatchOptions.ReadIdleTimeout` bounds the wait between inbound WS frames before Watch tears the connection down. Defaults to 75s (`DefaultWatchReadIdleTimeout`); set negative to disable. Implemented as a per-iteration `context.WithTimeout` on the underlying `coder/websocket` Read — verified that the library converts ctx-cancel into a connection close via `context.AfterFunc`, so Watch already exits cleanly on caller cancel; the new bound covers the silent-server / half-open-socket case the Phase 4 audit (#601) flagged.
