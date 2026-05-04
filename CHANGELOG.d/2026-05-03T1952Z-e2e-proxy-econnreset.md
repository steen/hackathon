### Fixed

- `tests/e2e/playwright/proxy.mjs`: attach `error` listeners to both halves of the upgraded WebSocket pipe so a stray `ECONNRESET` (concurrent socket closes during browser teardown — e.g. `useMessages` + `usePresence`) destroys the other half quietly instead of bubbling out as an unhandled `error` event and crashing the proxy/runner. (2026-05-03T19:52Z)
