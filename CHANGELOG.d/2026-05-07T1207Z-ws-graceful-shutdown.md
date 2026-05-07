### Changed

- WebSocket subscribers now receive a typed `1001 Going Away` close frame (reason `server_shutdown`) on `SIGINT`/`SIGTERM` instead of an abrupt TCP teardown. The browser sees a clean close code on every redeploy — no more `1006 abnormal closure` noise. The hub waits up to 2s (hard-coded) for in-flight close frames to flush before `srv.Shutdown` runs.
