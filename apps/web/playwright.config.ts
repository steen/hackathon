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
  // Per-test cap: worker-reported median is ~1.6s/test, so 30s gives ~18x
  // headroom while killing genuinely stuck waits in single-digit seconds
  // rather than the previous 60s. See #651.
  timeout: 30_000,
  expect: { timeout: 5_000 },
  // Whole-suite cap so a runaway can never hold a CI runner past 5 min;
  // the step-level `timeout-minutes: 8` in ci.yml is a belt-and-braces
  // outer bound covering Playwright launch + browser install fallout.
  globalTimeout: 5 * 60_000,
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
    // WebKit runs ONLY web-mobile.spec.ts (the iOS-class viewport
    // regression). Full cross-browser coverage of web.spec.ts +
    // presence.spec.ts would blow the <5min CI budget that #634 set.
    // iOS Safari is the production target for mobile users, so we
    // pay the cost only for the spec that asserts mobile-specific
    // layout. See #643.
    //
    // `iPhone 13` (vs `Desktop Safari`) gives a real iOS profile:
    // mobile UA, dpr=3, isMobile=true, hasTouch=true. The spec calls
    // page.setViewportSize() per-AC (375x667 phone, 768x1024 tablet),
    // which overrides the device's 390x664 default. See #647.
    {
      name: "webkit",
      testMatch: /web-mobile\.spec\.ts$/,
      use: { ...devices["iPhone 13"] },
    },
  ],
});
