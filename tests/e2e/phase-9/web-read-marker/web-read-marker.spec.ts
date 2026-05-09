// Phase 9 #944 — Web `useReadMarker` debounce + focus-flush, end-to-end.
//
// Discovery: this file is OUTSIDE `tests/e2e/playwright/` but is picked
// up because `apps/web/playwright.config.ts` lists
// `phase-9/web-read-marker/**/*.spec.ts` in its `testMatch`. The
// sibling phase-9 placeholder dirs (`web-channel-badges/`, `web-dms/`)
// stay test.skip-only and explicitly NOT matched until each gets its
// own follow-up. If you move or rename this file, update the config's
// `testMatch` accordingly.
//
// Behaviour pinned (decision-log `lt -p direct-messages 3` §22 / L22):
//
//   1. Rapid `markRead` calls collapse to one outgoing POST within the
//      250ms trailing-debounce window.
//   2. Tab visibility-change to "visible" (and window focus return)
//      flushes a pending advance immediately, before the 250ms window
//      would have elapsed.
//
// Hook-level coverage that already runs under vitest covers the same
// transitions in jsdom (`apps/web/src/hooks/useReadMarker.test.tsx` —
// 11 cases). This spec is the real-browser confirmation: same-origin
// proxy, real Chromium event loop, real WS frames driving the
// `markRead` call from Chat.tsx's effect.

import { expect, test, type Request, type Page } from "@playwright/test";

import { TOKEN_KEY } from "../../../../apps/web/src/api";
import {
  baseUrl,
  createChannelViaApi,
  registerViaApi,
  uniqueUsername,
  waitForChatShell,
} from "../../playwright/helpers";

async function postViaApi(token: string, channelId: string, body: string): Promise<void> {
  const res = await fetch(baseUrl() + `/api/channels/${encodeURIComponent(channelId)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ body }),
  });
  if (!res.ok) throw new Error(`post message failed: ${String(res.status)}`);
}

// Seeds the given JWT into localStorage on the SPA origin and reloads
// so AuthContext picks it up — bypasses POST /api/auth/login. The
// per-IP login limiter is Burst=10/5min (no env override per
// ratelimit/iplimit.go LoginIPConfig); the existing specs already
// crowd that bucket and adding two browser logins from the same IP
// would tip the suite over (#559, #744). chat-ui.spec.ts and
// aria-log.spec.ts use this same pattern.
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

// Records every POST /api/channels/{id}/read request the page emits, so a
// test can assert how many fired in a window. Returns a snapshot getter
// plus a per-channel filter so the assertions stay focused on the
// channel under test (channel create/list calls aren't reads).
function trackReadPosts(page: Page): {
  all: () => Request[];
  forChannel: (id: string) => Request[];
} {
  const seen: Request[] = [];
  page.on("request", (req) => {
    if (req.method() !== "POST") return;
    const u = new URL(req.url());
    if (!/^\/api\/channels\/[^/]+\/read$/.test(u.pathname)) return;
    seen.push(req);
  });
  return {
    all: () => [...seen],
    forChannel: (id: string) =>
      seen.filter(
        (r) => new URL(r.url()).pathname === `/api/channels/${encodeURIComponent(id)}/read`,
      ),
  };
}

test.describe("Web read-marker e2e (#944)", () => {
  // AC 1: a burst of inbound messages produces one POST /read, not one per
  // message. The viewer joins an empty channel; a second user then posts
  // five messages back-to-back via the REST API. Each WS arrival re-runs
  // Chat.tsx's effect → calls `markRead(latestCommittedId)`, which the
  // 250ms trailing debounce collapses. The test snapshots the POST count
  // before the burst and asserts exactly one new POST fires after.
  test("AC: rapid markRead calls collapse to one POST /api/channels/{id}/read in 250ms", async ({
    page,
  }) => {
    const viewerName = uniqueUsername("u-rm-viewer");
    const posterName = uniqueUsername("u-rm-poster");
    const viewer = await registerViaApi(viewerName);
    const poster = await registerViaApi(posterName);
    const channel = await createChannelViaApi(viewer.token, uniqueUsername("ch-rm"));

    const reads = trackReadPosts(page);

    await loginViaToken(page, viewer.token, viewerName);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    // Drain any read POST that may have fired on initial mount (empty
    // channel = no committed message id, so typically zero — but a
    // future change to seed a sentinel id would still pass this test as
    // long as the burst itself collapses). Wait the full 250ms + slack
    // to ensure any pending pre-burst timer has flushed.
    await page.waitForTimeout(400);
    const before = reads.forChannel(channel.id).length;

    // Fire five posts concurrently. Server-side they serialize behind
    // the channel mutex, but the resulting WS frames hit the viewer in
    // tight succession (LAN-local fixture, single Go process). All five
    // updates to `latestCommittedMessageId` therefore land inside one
    // 250ms debounce window.
    const bodies = Array.from({ length: 5 }, (_, i) => `burst-${String(i)}-${String(Date.now())}`);
    await Promise.all(bodies.map((b) => postViaApi(poster.token, channel.id, b)));

    // Wait for the last message to render — confirms the WS pipeline
    // has delivered every frame before we evaluate the debounce window.
    const lastBody = bodies[bodies.length - 1];
    if (lastBody === undefined) throw new Error("bodies array empty");
    await expect(page.locator('[data-testid="msg"]', { hasText: lastBody })).toBeVisible({
      timeout: 10_000,
    });

    // Wait one full debounce window past the last arrival so the
    // trailing POST has had time to fire. Padding accounts for
    // setTimeout drift + browser → proxy → fixture round-trip.
    await page.waitForTimeout(600);

    const after = reads.forChannel(channel.id).length;
    const fired = after - before;

    // Five WS frames within 250ms must collapse to exactly one POST. A
    // regression that drops the debounce (or shortens the window so the
    // last frame slips past) would show 2+ here. Zero would mean the
    // hook never scheduled, which the AC explicitly rules out.
    expect(fired).toBe(1);
  });

  // AC 2: a focus / visibilitychange event flushes a pending advance
  // immediately, well before 250ms would have elapsed. Drive a single
  // markRead by posting one message, then dispatch the focus signals
  // and assert the resulting POST lands inside a sub-debounce timeout.
  // `waitForRequest` rejects after 200ms — strictly less than the
  // 250ms trailing-debounce window — so a passing run proves the
  // flush is event-driven, not a timer coincidence.
  test("AC: tab focus flushes the pending advance before the 250ms window elapses", async ({
    page,
  }) => {
    const viewerName = uniqueUsername("u-rm-foc-v");
    const posterName = uniqueUsername("u-rm-foc-p");
    const viewer = await registerViaApi(viewerName);
    const poster = await registerViaApi(posterName);
    const channel = await createChannelViaApi(viewer.token, uniqueUsername("ch-rm-foc"));

    const reads = trackReadPosts(page);

    await loginViaToken(page, viewer.token, viewerName);
    await page.getByRole("button", { name: `#${channel.name}` }).click();
    await page.waitForTimeout(400);
    const before = reads.forChannel(channel.id).length;

    // Single post — useReadMarker schedules a 250ms timer. We do NOT
    // wait the full debounce window before dispatching focus, otherwise
    // the timer would beat the flush and we'd be asserting nothing.
    const body = `focus-flush-${String(Date.now())}`;
    await postViaApi(poster.token, channel.id, body);
    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 10_000,
    });

    // Race the debounce: arm the request waiter at <250ms, then
    // dispatch the focus signals. If the flush hook is wired correctly
    // the request lands within network-latency time. The 200ms ceiling
    // is a hard upper bound on "flush-was-faster-than-timer".
    const flushedReq = page.waitForRequest(
      (req) =>
        req.method() === "POST" &&
        new URL(req.url()).pathname === `/api/channels/${encodeURIComponent(channel.id)}/read`,
      { timeout: 200 },
    );
    // Fire BOTH visibilitychange→visible and window focus. The hook
    // listens to both (legacy Safari quirk per useReadMarker.ts);
    // Playwright tabs are typically already-focused, so a stand-alone
    // `focus` event may be a no-op on some UAs. Dispatching both keeps
    // the assertion robust without changing what's being asserted.
    await page.evaluate(() => {
      Object.defineProperty(document, "visibilityState", {
        configurable: true,
        get: () => "visible",
      });
      document.dispatchEvent(new Event("visibilitychange"));
      window.dispatchEvent(new Event("focus"));
    });

    await flushedReq;

    const after = reads.forChannel(channel.id).length;
    expect(after - before).toBe(1);
  });
});
