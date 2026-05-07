### Changed

- `apps/server/probe.go` now relies solely on `http.Client.Timeout` for the
  1.5s health-probe deadline; the redundant `context.WithTimeout` wrapper
  has been removed. Both deadlines fired at the same wall-clock instant,
  so the change is observably equivalent. (#853)
