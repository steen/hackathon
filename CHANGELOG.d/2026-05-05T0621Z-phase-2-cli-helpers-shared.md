### Changed

- **tests/e2e**: extracted the `chatd` subprocess plumbing (binary
  build cache, `chatd login` / `chatd watch` wrappers, fixture
  generators) shared between
  `tests/e2e/phase-2/cli-full-commands/` and
  `tests/e2e/phase-2/presence/` into a new
  `tests/e2e/internal/clihelp/` package so future phase-2/3 ACs that
  exec the CLI don't grow a third copy. No behavior change to existing
  tests; the chatd binary now compiles once per `go test ./...` run
  across both packages instead of twice. Closes #380.
  (2026-05-05T06:21Z)
