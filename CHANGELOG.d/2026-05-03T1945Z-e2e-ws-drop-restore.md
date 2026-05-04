### Added

- **tests/e2e/playwright/web.spec.ts**: re-enabled the WS drop+restore
  Playwright scenario (previously `test.skip` per #102) using
  `page.routeWebSocket` (Playwright 1.48+). The test forwards traffic to
  the real server, then closes every server-side handle to simulate a
  transport-level drop, asserts the connection badge transitions to
  Reconnecting and back to Connected within 5s, and that a message posted
  by another client after reconnect arrives over the renewed
  subscription. Replaces the flaky `setOffline(true)` approach.
  Closes #104. (2026-05-03T19:45Z)
