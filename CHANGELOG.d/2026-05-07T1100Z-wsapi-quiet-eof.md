### Fixed

- `apps/server/internal/wsapi/handler.go`: ungraceful WS disconnects (browser tab close, network drop, process kill) used to log a `WARN level=ws read err="failed to read frame header: EOF"` line for each disconnect — visible during every Playwright run and on every real page navigation. The disconnect is expected; downgrade `io.EOF` and `net.ErrClosed` from `slog.Warn` to `slog.Debug` so prod logs stay quiet. Genuine read errors (non-EOF, non-closed) still surface at WARN.
