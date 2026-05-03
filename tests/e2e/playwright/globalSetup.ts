// Sanity-checks that the fixture-bootstrap wrapper (runWeb.mjs) ran first
// and exported E2E_BASE_URL / E2E_INVITE_CODE / PW_BASE_URL. The contract
// has to be primed at the wrapper level — globalSetup only enforces the
// expected env-var set here so a typo'd entry script fails loudly.

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
