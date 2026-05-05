import { test, expect } from "@playwright/test";

import { createChannelViaApi, loginInBrowser, registerViaApi, uniqueUsername } from "./helpers";

// Regression-guard for #559. PR #557 dropped the explicit aria-live="polite"
// from the messages-list <div> and now relies on role="log"'s implicit
// polite live-region semantic per ARIA 1.2. The structural contract is
// covered in jsdom by apps/web/src/routes/Chat.test.tsx; this spec re-asserts
// the same contract under real Chromium so the production bundle can't
// silently re-acquire an explicit aria-live in a future refactor.
test.describe("Messages list ARIA contract", () => {
  test("messages-list role=log carries implicit aria-live=polite (no explicit attr)", async ({
    page,
  }) => {
    const username = uniqueUsername("u-aria-log");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    await loginInBrowser(page, username);
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
