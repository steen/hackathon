import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["**/*.{test,spec}.?(c|m)[jt]s?(x)", "**/*_test.?(c|m)[jt]s?(x)"],
    // tests/e2e is its own pnpm workspace package with its own vitest
    // config (and a separate Playwright suite) — keep it out of the
    // root tests-package run so `pnpm -r test` doesn't double-execute
    // the e2e specs (which want their own server fixture lifecycle).
    exclude: ["e2e/**", "**/node_modules/**", "**/dist/**"],
    testTimeout: 120_000,
    hookTimeout: 120_000,
  },
});
