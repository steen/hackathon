### Changed

- **apps/web**: Gated the `window.__chatd` WS-transition test hook in
  `apps/web/src/main.tsx` behind `import.meta.env.MODE !== "production"`
  so it no longer ships in real production bundles. The hook (used by the
  Playwright suite to assert the `closed → connecting → open` sequence
  after a WS drop) is now installed only in dev/test/e2e builds. The e2e
  harness (`tests/e2e/playwright/runWeb.mjs`) was updated to call a new
  `build:e2e` script that runs `vite build --mode e2e`, so MODE stays
  non-production for the bundle Playwright loads. Closes #658.
  (2026-05-05T12:29Z)
