### Added

- **apps/web**: Regression-guard vitest fixture
  `apps/web/src/__tests__/prod-bundle-hygiene.test.ts` that runs
  `pnpm run build` in production mode and asserts no built artifact
  under `dist/assets/*.js` contains the literal `__chatd`. The
  `window.__chatd` WS-transition test hook is gated on
  `import.meta.env.MODE !== "production"` (#693), and this test fails
  if a future commit drops the gate. Skipped by default to keep dev
  `pnpm test` fast; enabled in CI by `RUN_PROD_BUNDLE_HYGIENE=1` on
  the pnpm-job test step. Closes #699. (2026-05-05T12:58Z)
