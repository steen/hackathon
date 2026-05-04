### Changed

- Server bootstrap split into `apps/server/main.go` (config + DB + run-loop) and `apps/server/internal/wiring/` (per-feature route registration). New server features add a `wiring/<feature>.go` file plus one line in `wiring.Build` instead of editing `main.go` — resolves the long-standing `main.go` conflict-magnet.
