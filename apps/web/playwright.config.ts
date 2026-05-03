import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

// Lives under apps/web (per #81 footprint), drives the Playwright suite that
// physically lives at tests/e2e/playwright. The split is intentional: the
// suite is logically a cross-app e2e (server + api-client + web), but the
// Playwright config has to sit next to the app it serves so `pnpm exec
// playwright test` from the web package finds its own browsers.

const here = dirname(fileURLToPath(import.meta.url));
const e2eDir = resolve(here, "..", "..", "tests", "e2e");

export default defineConfig({
  testDir: resolve(e2eDir, "playwright"),
  testMatch: /.*\.spec\.ts/,
  // Server fixture + Vite startup is slow on cold caches; one-shot retries
  // keep flaky CI from spinning forever while still surfacing real bugs on
  // the second failure.
  retries: process.env.CI ? 1 : 0,
  fullyParallel: false,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: process.env.CI ? [["list"]] : [["list"]],

  globalSetup: resolve(e2eDir, "playwright", "globalSetup.ts"),
  globalTeardown: resolve(e2eDir, "playwright", "globalTeardown.ts"),

  // No `webServer` block: the runWeb.mjs wrapper boots the Go fixture +
  // builds + serves apps/web via a same-origin proxy, then execs this
  // Playwright run. PW_BASE_URL points at the proxy.

  use: {
    baseURL: process.env.PW_BASE_URL ?? "http://127.0.0.1:5174",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
