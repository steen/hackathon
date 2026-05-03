// Fixture lifecycle is owned by runWeb.mjs (the wrapper that boots the Go
// server before Playwright starts). Teardown intentionally does nothing:
// the wrapper's own SIGTERM/exit hooks tear the fixture down regardless of
// whether Playwright finished cleanly.

import type { FullConfig } from "@playwright/test";

export default function globalTeardown(_config: FullConfig): void {
  // intentionally empty
}
