### Docs

- `docs/ops/runbook.md`: replace stale "No compose healthcheck" section with "In-image healthcheck" reflecting current state — `docker-compose.yml` and `Dockerfile` both declare a healthcheck via the binary's `--health-probe` flag (`apps/server/probe.go`). Drops the dangling reference to issue #796 (shipped).
