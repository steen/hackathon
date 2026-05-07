### Added

- New Playwright spec `tests/e2e/playwright/chat-ui.spec.ts` covering three Phase 6 contracts that none of the existing browser specs exercise:
  1. Offline sender renders by username, not ULID (verifies `/api/users` + the `usePresence` directory merge).
  2. Message meta-line orders `<time>` before `<span class="msg__sender">` (Phase 6 layout flip; pinned at the DOM level so a `flex-direction: row-reverse` regression also trips).
  3. Every sender span carries one of `msg__sender--user-{blue|green|purple|yellow}` (color-coded sender contract).

  Tests use the JWT-seed pattern from `aria-log.spec.ts` instead of `loginInBrowser` so they don't add weight to the per-IP login limiter (Burst=10) the existing specs already crowd.
