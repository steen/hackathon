### Added

- **tests/e2e/phase-1/access-log-fields-and-wiring/access_log_xff_ignored_test.go**:
  new black-box test for the unset-`CHAT_TRUSTED_PROXY` branch of AC-1
  in `specs/plans/phase-1/feature-access-log-fields-and-wiring.md`. Boots
  the binary without configuring a trusted proxy, sends a request
  carrying `X-Forwarded-For: 1.2.3.4, 5.6.7.8`, and asserts the access
  log records the loopback host (never an XFF entry). Locks in the safe
  default and guards against accidental XFF-trust regressions before the
  trusted-proxy parser lands. The positive branch
  (`CHAT_TRUSTED_PROXY=1` honors leftmost XFF) is deferred to the PR
  that introduces the parser. Closes #319. (2026-05-05T08:13Z)
