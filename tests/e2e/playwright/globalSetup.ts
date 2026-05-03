// Sanity-checks that the fixture-bootstrap wrapper (runWeb.mjs) ran first
// and exported E2E_BASE_URL / E2E_INVITE_CODE / VITE_API_BASE_URL. Playwright
// starts its `webServer` (the Vite dev server) BEFORE globalSetup, so the
// fixture has to be primed at the wrapper level — globalSetup only enforces
// the contract here so a typo'd entry script fails loudly.

import type { FullConfig } from "@playwright/test";

export default function globalSetup(_config: FullConfig): void {
  for (const k of ["E2E_BASE_URL", "E2E_INVITE_CODE", "PW_BASE_URL"]) {
    const v = process.env[k];
    if (v === undefined || v === "") {
      throw new Error(
        `${k} not set — run \`pnpm e2e:web\` (which uses runWeb.mjs to boot the fixture before Playwright)`,
      );
    }
  }
}
