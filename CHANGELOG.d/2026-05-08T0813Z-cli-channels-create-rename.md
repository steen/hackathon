### Added

- `chatd channels create <name>` and `chatd channels rename <current-name> <new-name>` sub-subcommands. Both print `<id>\t<name>` to stdout on success and exit 1 with a stderr message on rejection (400 invalid name, 403 on `#general`, 404 unknown channel, 409 duplicate name, 429 rate-limited). Names are validated locally against the same regex the server enforces (`^[a-z0-9][a-z0-9-]{0,39}$`) so obvious typos short-circuit before a round-trip. Dispatch lives inside `apps/cli/cmd/channels.go`; `apps/cli/main.go` is untouched. Closes #845.
