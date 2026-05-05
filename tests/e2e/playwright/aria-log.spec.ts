import { test, expect } from "@playwright/test";

import { createChannelViaApi, registerViaApi, uniqueUsername } from "./helpers";

// Regression-guard for #559. PR #557 dropped the explicit aria-live="polite"
// from the messages-list <div> and now relies on role="log"'s implicit
// polite live-region semantic per ARIA 1.2. The structural contract is
// covered in jsdom by apps/web/src/routes/Chat.test.tsx; this re-asserts
// the same contract under real Chromium so the production bundle can't
// silently re-acquire an explicit aria-live in a future refactor.
//
// We bypass loginInBrowser (which posts to /api/auth/login) by seeding the
// JWT into localStorage directly: the per-IP login limiter is at Burst=10
// and several existing specs already crowd the bucket, so adding one more
// browser login here pushes the suite over the cliff. See PRD §9 + #559.
test.describe("Messages list ARIA contract", () => {
  test("messages-list role=log carries implicit aria-live=polite (no explicit attr)", async ({
    page,
  }) => {
    const username = uniqueUsername("u-aria-log");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    // Seed the token under the same key apps/web/src/api.ts uses
    // (TOKEN_KEY = "hackathon.token") so AuthContext reads it on mount and
    // the app boots already-authenticated. This avoids POSTing
    // /api/auth/login, which matters because the per-IP login limiter is
    // Burst=10 and the existing specs already crowd that bucket — a 12th
    // login from this IP returns 429 and tips the suite over (see #559).
    // page.goto("/") uses Playwright's baseURL (PW_BASE_URL = the proxy
    // origin) so localStorage is set on the same origin the SPA later
    // reads from.
    await page.goto("/");
    await page.evaluate((t) => {
      window.localStorage.setItem("hackathon.token", t);
    }, reg.token);
    // Full reload (not a hash change) so AuthProvider's mount initializer
    // reads the just-seeded token instead of the original null.
    await page.reload();
    await expect(page.locator(".sidebar strong")).toContainText(username, { timeout: 10_000 });

    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const list = page.getByTestId("message-list");
    await expect(list).toBeVisible();
    await expect(list).toHaveAttribute("role", "log");
    // Any aria-live value (even "off") would fail this — role="log" must be
    // the single source of truth for the announcement priority.
    await expect(list).not.toHaveAttribute("aria-live", /.*/);
    await expect(list).toHaveAttribute("aria-relevant", "additions");
    await expect(list).toHaveAttribute("aria-atomic", "false");
  });
});
