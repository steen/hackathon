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
    // Phase-10 L25: viewer registered before the channel exists, so
    // the §9 auto-add at registration does not cover them. Invite
    // explicitly under the public-channel auto-fill carve-out.
    const channel = await createChannelViaApi(sender.token, uniqueUsername("ch"), [viewer.user.id]);

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

  // Senders render in distinct inline colors via `userColor(name)`
  // — an OKLCH hash of the visible username. Two distinct authors
  // must produce two distinct rendered colors. Regressions to watch:
  //   - dropping the inline style entirely (every sender becomes the
  //     default text color on the panel)
  //   - reverting to a small fixed-class palette (users >palette-size
  //     collide on the same color)
  test("AC: distinct senders render in distinct inline colors", async ({ page }) => {
    const author1 = uniqueUsername("u-color-A");
    const author2 = uniqueUsername("u-color-B");
    const reg1 = await registerViaApi(author1);
    const reg2 = await registerViaApi(author2);
    // Phase-10 L25: reg2 needs to be a channel member to post; the
    // §9 auto-add at registration only covers users who registered
    // AFTER the channel existed.
    const channel = await createChannelViaApi(reg1.token, uniqueUsername("ch"), [reg2.user.id]);

    const body1 = `color-A-${String(Date.now())}`;
    const body2 = `color-B-${String(Date.now())}`;
    await postViaApi(reg1.token, channel.id, body1);
    await postViaApi(reg2.token, channel.id, body2);

    await loginViaToken(page, reg1.token, author1);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const colors = new Set<string>();
    for (const body of [body1, body2]) {
      const row = page.locator('[data-testid="msg"]', { hasText: body });
      await expect(row).toBeVisible({ timeout: 10_000 });
      const style = (await row.locator(".msg__sender").getAttribute("style")) ?? "";
      // userColor() writes `color: oklch(...)` inline. Empty style would
      // mean the function isn't being called at all.
      expect(style).toMatch(/color:\s*oklch\(/);
      colors.add(style);
    }
    // Two unique usernames → two unique colors. cyrb53 % 360 collides
    // ~3% of the time for a randomly-picked pair; uniqueUsername adds
    // ~9 chars of entropy on each side, so collisions are essentially
    // never the cause of a real test failure.
    expect(colors.size).toBe(2);
  });

  // Day-divider rule: a banner separates messages whose local dates
  // differ. The very first message also gets a divider so the reader
  // can anchor "what day am I reading?". The cross-midnight branch is
  // covered by a unit test in apps/web/src/routes/Chat.test.tsx that
  // pins three messages spanning a real local-day boundary; this
  // browser test pins only the integration shape: a single message
  // produces exactly one visible day divider.
  test("AC: day-divider renders above the first history message", async ({ page }) => {
    const author = uniqueUsername("u-div");
    const reg = await registerViaApi(author);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    const body = `today-${String(Date.now())}`;
    await postViaApi(reg.token, channel.id, body);

    await loginViaToken(page, reg.token, author);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 10_000,
    });
    const dividers = page.locator('[data-testid="day-divider"]');
    await expect(dividers).toHaveCount(1);
  });
});
