// Phase 3 mobile-first regression coverage. Issue #634.
//
// Asserts the existing CSS in apps/web/src/styles.css (shipped under #612 +
// #626) keeps the chat flow usable at phone (375x667) and tablet (768x1024)
// viewports. The spec is layout-only: it runs the same login → select
// channel → send happy path as web.spec.ts and asserts the visible message,
// plus a layout-transition check across the 768px breakpoint.
//
// Drawer toggle behaviour is intentionally NOT asserted — that thread was
// de-scoped from #156 via #613/#619. We only regression-cover the
// already-shipped pieces:
//   - single-column stack below 768px (sidebar above messages)
//   - two-column side-by-side at 768px and above
//   - 44px minimum height on .sidebar li button + .composer button +
//     .composer textarea on phone widths
//
// Chromium only (per #634); WebKit/iOS Safari deliberately deferred to keep
// CI under budget. If WebKit coverage is needed it should be filed as a
// follow-up sub-issue under #448.

import { test, expect } from "@playwright/test";

import { createChannelViaApi, loginInBrowser, registerViaApi, uniqueUsername } from "./helpers";

// 44px is the iOS HIG / WCAG 2.5.5 minimum tap target. Mirrors the
// `min-height: 44px` rule the @media (max-width: 767px) block applies to
// .sidebar li button, .composer button, and .composer textarea.
const MIN_TAP_TARGET_PX = 44;

const PHONE = { width: 375, height: 667 } as const; // iPhone SE class
const TABLET = { width: 768, height: 1024 } as const; // iPad class

test.describe("Web mobile/tablet viewport regression (#634)", () => {
  test("AC: phone 375x667 — login → channel → send echoes back; layout single-column", async ({
    page,
  }) => {
    await page.setViewportSize({ width: PHONE.width, height: PHONE.height });

    const username = uniqueUsername("u-mobile");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    await loginInBrowser(page, username);

    // Below 768px the @media block stacks sidebar above messages
    // (grid-template-rows: auto 1fr). Verify by bounding boxes: sidebar's
    // bottom edge must be at or above the messages region's top edge.
    const sidebar = page.locator(".sidebar");
    const messages = page.locator(".messages");
    await expect(sidebar).toBeVisible();
    await expect(messages).toBeVisible();
    const sidebarBox = await sidebar.boundingBox();
    const messagesBox = await messages.boundingBox();
    if (sidebarBox === null || messagesBox === null) {
      throw new Error("sidebar/messages bounding box missing");
    }
    expect(sidebarBox.y + sidebarBox.height).toBeLessThanOrEqual(messagesBox.y + 1);

    // Channel-list buttons must hit the 44px tap-target minimum (PR #612).
    const channelButton = page.getByRole("button", { name: `#${channel.name}` });
    const channelBox = await channelButton.boundingBox();
    if (channelBox === null) throw new Error("channel button bounding box missing");
    expect(channelBox.height).toBeGreaterThanOrEqual(MIN_TAP_TARGET_PX);

    await channelButton.click();

    // Composer textarea + Send button must hit 44px on phone widths (PR #626).
    const sendButton = page.getByRole("button", { name: /^send$/i });
    const composerInput = page.getByLabel("message");
    const sendBox = await sendButton.boundingBox();
    const composerBox = await composerInput.boundingBox();
    if (sendBox === null || composerBox === null) {
      throw new Error("composer bounding box missing");
    }
    expect(sendBox.height).toBeGreaterThanOrEqual(MIN_TAP_TARGET_PX);
    expect(composerBox.height).toBeGreaterThanOrEqual(MIN_TAP_TARGET_PX);

    const body = `mobile-${String(Date.now())}`;
    await composerInput.fill(body);
    await sendButton.click();

    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("AC: tablet 768x1024 — layout transitions to two-column; send still works", async ({
    page,
  }) => {
    await page.setViewportSize({ width: TABLET.width, height: TABLET.height });

    const username = uniqueUsername("u-tablet");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    await loginInBrowser(page, username);

    // At exactly 768px the @media (max-width: 767px) block does NOT apply,
    // so .chat-layout reverts to its 240px 1fr grid. Verify by bounding
    // boxes: sidebar's right edge must sit at or before messages' left edge.
    const sidebar = page.locator(".sidebar");
    const messages = page.locator(".messages");
    await expect(sidebar).toBeVisible();
    await expect(messages).toBeVisible();
    const sidebarBox = await sidebar.boundingBox();
    const messagesBox = await messages.boundingBox();
    if (sidebarBox === null || messagesBox === null) {
      throw new Error("sidebar/messages bounding box missing");
    }
    expect(sidebarBox.x + sidebarBox.width).toBeLessThanOrEqual(messagesBox.x + 1);
    // And they must share a horizontal band — i.e. the sidebar's bottom is
    // not above the messages' top, which would imply we're still stacked.
    expect(sidebarBox.y + sidebarBox.height).toBeGreaterThan(messagesBox.y);

    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const body = `tablet-${String(Date.now())}`;
    await page.getByLabel("message").fill(body);
    await page.getByRole("button", { name: /^send$/i }).click();

    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 10_000,
    });
  });
});
