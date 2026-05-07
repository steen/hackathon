// Phase 6 (chat-ui extraction) regression coverage. None of these
// behaviours are exercised by the existing browser specs:
//
//   - Offline-username resolution. /api/users is fetched in parallel
//     with /api/presence on Chat mount and merged into the username
//     directory; messages from senders who have since disconnected
//     therefore still render the username, not the raw ULID.
//   - Message meta-line layout: <time> precedes <span class="msg__sender">
//     inside .msg__meta. Phase 6 flipped this from the original
//     "sender then time" order to match the reference screenshot.
//   - Sender color class: every sender span carries one of
//     `msg__sender--user-blue|green|purple|yellow`, picked by
//     `userColorClass(sender_user_id)` (deterministic hash mod 4).
//
// Each test below pins one of the three contracts so a future
// refactor that drops /api/users, swaps the meta-line order, or
// removes the color classes flips this suite red.
//
// We bypass loginInBrowser (which posts to /api/auth/login) by seeding
// the JWT into localStorage directly: the per-IP login limiter is at
// Burst=10 and the existing specs already crowd the bucket; adding
// three more browser logins from this IP would tip the suite over
// (see #559, #744). The pattern mirrors aria-log.spec.ts.

import { expect, test, type Page } from "@playwright/test";

import { TOKEN_KEY } from "../../../apps/web/src/api";
import {
  baseUrl,
  createChannelViaApi,
  registerViaApi,
  uniqueUsername,
  waitForChatShell,
} from "./helpers";

async function postViaApi(token: string, channelId: string, body: string): Promise<void> {
  const res = await fetch(baseUrl() + `/api/channels/${encodeURIComponent(channelId)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ body }),
  });
  if (!res.ok) throw new Error(`post message failed: ${String(res.status)}`);
}

// Seeds the given JWT into localStorage on the SPA origin and reloads
// so AuthContext picks it up. Mirrors the pattern in aria-log.spec.ts.
async function loginViaToken(page: Page, token: string, username: string): Promise<void> {
  await page.goto("/");
  await page.evaluate(
    ([key, t]) => {
      window.localStorage.setItem(key, t);
    },
    [TOKEN_KEY, token],
  );
  await page.reload();
  await waitForChatShell(page, username);
}

test.describe("Phase 6: chat-ui extraction regression guards", () => {
  // Reproduces the bug surfaced during Phase 6 manual testing where
  // resolveSender fell back to the raw ULID for senders absent from the
  // /api/presence response. /api/users now ships, usePresence merges
  // both directories, and the offline sender's username resolves.
  test("AC: offline sender renders by username, not raw ULID", async ({ page }) => {
    const senderName = uniqueUsername("u-off-sender");
    const viewerName = uniqueUsername("u-off-viewer");
    const sender = await registerViaApi(senderName);
    const viewer = await registerViaApi(viewerName);
    const channel = await createChannelViaApi(sender.token, uniqueUsername("ch"));

    // Sender posts purely via the REST API — never opens a WS, so they
    // are NOT in the /api/presence response when the viewer's Chat
    // mounts. The username has to come from /api/users.
    const body = `offline-${String(Date.now())}`;
    await postViaApi(sender.token, channel.id, body);

    await loginViaToken(page, viewer.token, viewerName);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const row = page.locator('[data-testid="msg"]', { hasText: body });
    await expect(row).toBeVisible({ timeout: 10_000 });

    // Sender span must render the username. The ULID would be the
    // raw 26-char Crockford string; a regression that drops
    // /api/users (or its merge into knownUsernames) would fall back
    // to the id and trip the `not.toHaveText` below.
    const senderSpan = row.locator(".msg__sender");
    await expect(senderSpan).toHaveText(senderName);
    await expect(senderSpan).not.toHaveText(sender.user.id);
  });

  // Phase 6 flipped the meta line from `sender, badges, time` to
  // `time, sender, badges` to match the reference screenshot. The
  // structural assertion below pins the new order at the DOM level
  // so a future refactor (or a stylesheet-only "fix" that reorders
  // visually via flex `order:`) can't silently re-break it.
  test("AC: message meta-line orders <time> before <span class=msg__sender>", async ({ page }) => {
    const username = uniqueUsername("u-meta-order");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    // Post via API so the message lands as a settled history row, not
    // an optimistic-send pending row (those go through a different
    // layout branch in MessageItem).
    const body = `meta-${String(Date.now())}`;
    await postViaApi(reg.token, channel.id, body);

    await loginViaToken(page, reg.token, username);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const row = page.locator('[data-testid="msg"]', { hasText: body });
    await expect(row).toBeVisible({ timeout: 10_000 });

    // Compare DOM positions instead of bounding boxes so a CSS
    // `flex-direction: row-reverse` or `order:` regression (visually
    // flipped, DOM unchanged) still trips this — the ARIA reading
    // order also depends on DOM order, which is what we want to
    // preserve.
    const positions = await row.locator(".msg__meta").evaluate((meta: HTMLElement) => {
      const time = meta.querySelector("time");
      const sender = meta.querySelector(".msg__sender");
      if (time === null || sender === null) {
        return { timeIdx: -1, senderIdx: -1 };
      }
      const children = Array.from(meta.children);
      return {
        timeIdx: children.indexOf(time),
        senderIdx: children.indexOf(sender),
      };
    });
    expect(positions.timeIdx).toBeGreaterThanOrEqual(0);
    expect(positions.senderIdx).toBeGreaterThanOrEqual(0);
    expect(positions.timeIdx).toBeLessThan(positions.senderIdx);
  });

  // Senders are color-coded via userColorClass(senderId) → one of
  // four `msg__sender--user-{blue|green|purple|yellow}` classes.
  // We assert that every sender span carries one — a regression
  // that drops the userColorClass call leaves senders uncolored.
  test("AC: every sender span carries one msg__sender--user-* color class", async ({ page }) => {
    const author1 = uniqueUsername("u-color-A");
    const author2 = uniqueUsername("u-color-B");
    const reg1 = await registerViaApi(author1);
    const reg2 = await registerViaApi(author2);
    const channel = await createChannelViaApi(reg1.token, uniqueUsername("ch"));

    const body1 = `color-A-${String(Date.now())}`;
    const body2 = `color-B-${String(Date.now())}`;
    await postViaApi(reg1.token, channel.id, body1);
    await postViaApi(reg2.token, channel.id, body2);

    // View through author1 — pulls history including author2's row,
    // exercising the same color-class branch on a non-self sender.
    await loginViaToken(page, reg1.token, author1);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    for (const body of [body1, body2]) {
      const row = page.locator('[data-testid="msg"]', { hasText: body });
      await expect(row).toBeVisible({ timeout: 10_000 });
      const classes = (await row.locator(".msg__sender").getAttribute("class")) ?? "";
      expect(classes).toMatch(/\bmsg__sender--user-(blue|green|purple|yellow)\b/);
    }
  });
});
