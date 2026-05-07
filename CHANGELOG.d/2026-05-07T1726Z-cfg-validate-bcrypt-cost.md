### Changed

- `config.Validate` now parses `CHAT_BCRYPT_COST` and stores the result on `Config.BcryptCost`, replacing the standalone parse-and-set step in `apps/server/main.go::run`. A bad value now surfaces through the same path as other config errors (with prior `jwt_secret_present_and_strong`, `invite_code_present`, `bind_address_loopback_or_overridden` checks already on the slice), and the success path adds a new `bcrypt_cost_within_range` line to the existing "config check ok name=…" output. Closes #830, #828.
