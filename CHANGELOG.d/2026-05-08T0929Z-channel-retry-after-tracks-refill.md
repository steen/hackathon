### Fixed

- `apps/server/internal/wiring`: the per-user channel-write 429 response's `Retry-After` header now tracks the configured refill cadence. Previously `registerChannels` passed a hardcoded `time.Minute` to `UserRateLimit`, so operators setting `CHAT_CHANNEL_WRITE_REFILL` (e.g. to `30s` or `2m`) got a header that disagreed with the actual bucket refill — well-behaved clients backing off on `Retry-After` either retried too early or waited too long. The wiring now passes `writeCfg.Refill` (the env-resolved value already in scope) so the header matches the cadence. Closes #898.
