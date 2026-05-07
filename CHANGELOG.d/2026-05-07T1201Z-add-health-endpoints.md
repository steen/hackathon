### Added

- `apps/server`: add `GET /healthz` (process-up liveness, always 200) and `GET /readyz` (DB-bound readiness — 200 when `Repo.DB().PingContext` succeeds within 1s, 503 with `code: "not_ready"` on ping failure). Both responses use the standard `{ok,data,error}` envelope. In the no-DB phase-0 boot path (`Deps.Repo == nil`) `/readyz` returns 200 since there is no DB to be unready against. Wired through new `apps/server/internal/wiring/health.go`; `/healthz` and `/readyz` added to `reservedAPITopLevelPrefixes` so a typo'd machine probe gets a JSON 404 instead of SPA HTML.
