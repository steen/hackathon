### Fixed

- `scripts/check-env-example.mjs`: anchor the legacy `<lowercase>Env = "..."` regex to the body of `apps/server/main.go`'s first package-level `const ( ... )` block instead of the whole file. A future `var sneakyEnv = "CHAT_..."` or function-local sentinel can no longer leak into the required-set. The script now exits 2 with a clear message if no const block is found. Also refresh the stale top-of-file line citation (config.go env consts live at lines 17-25, not 17-31).
