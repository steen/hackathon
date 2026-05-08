### Changed

- Server logs an `info`-level line on startup naming the active per-user channel-write rate limiter config (burst + refill), so operators can confirm what's in effect without inspecting the environment. The existing `warn` on non-default overrides is unchanged. (#884)
