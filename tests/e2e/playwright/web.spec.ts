import { test, expect, type BrowserContext, type WebSocketRoute } from "@playwright/test";

import {
  baseUrl,
  createChannelViaApi,
  loginInBrowser,
  registerViaApi,
  uniqueUsername,
} from "./helpers";

async function postViaApi(token: string, channelId: string, body: string): Promise<void> {
  const res = await fetch(baseUrl() + `/api/channels/${encodeURIComponent(channelId)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ body }),
  });
  if (!res.ok) throw new Error(`post message failed: ${String(res.status)}`);
}

test.describe("Web e2e (real browser via Playwright)", () => {
  test("AC: login → chat → posting a message echoes back via WS", async ({ page }) => {
    const username = uniqueUsername("u-web-echo");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    await loginInBrowser(page, username);
    // The sidebar lists channels; click ours to make it active.
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const body = `web-echo-${String(Date.now())}`;
    await page.getByLabel("message").fill(body);
    await page.getByRole("button", { name: /^send$/i }).click();

    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 10_000,
    });
  });

  test("AC: two browser contexts cross-receive messages without refresh", async ({ browser }) => {
    const u1 = uniqueUsername("u-web-A");
    const u2 = uniqueUsername("u-web-B");
    const r1 = await registerViaApi(u1);
    await registerViaApi(u2);
    const channel = await createChannelViaApi(r1.token, uniqueUsername("ch"));

    const ctxA: BrowserContext = await browser.newContext();
    const ctxB: BrowserContext = await browser.newContext();
    try {
      const pageA = await ctxA.newPage();
      const pageB = await ctxB.newPage();

      await loginInBrowser(pageA, u1);
      await loginInBrowser(pageB, u2);

      await pageA.getByRole("button", { name: `#${channel.name}` }).click();
      await pageB.getByRole("button", { name: `#${channel.name}` }).click();

      const body = `cross-ctx-${String(Date.now())}`;
      await pageA.getByLabel("message").fill(body);
      await pageA.getByRole("button", { name: /^send$/i }).click();

      // Sender's own client also echoes — but the assertion that matters is B.
      await expect(pageB.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
        timeout: 10_000,
      });
    } finally {
      await ctxA.close();
      await ctxB.close();
    }
  });

  // Drives a deterministic WS drop+restore via Playwright's
  // page.routeWebSocket (Playwright >= 1.48). The route forwards traffic to
  // the real server by default; the test calls server.close() at a known
  // point to simulate a transport-level disconnect. The api-client's
  // reconnect path then mints a fresh ticket and opens a new socket, which
  // routeWebSocket intercepts again and forwards. setOffline isn't used —
  // its mix of HTTP-blocked / sockets-killed semantics is what made the
  // original scenario flaky (see #104).
  test("AC: WS drops + restores → reconnect, post-outage message arrives", async ({ page }) => {
    const username = uniqueUsername("u-web-reconn");
    const other = uniqueUsername("u-web-other");
    const reg = await registerViaApi(username);
    const otherReg = await registerViaApi(other);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    // Track every server-side WS handle so we can close them on demand. New
    // sockets opened by the api-client's reconnect path land here too.
    const serverSides: WebSocketRoute[] = [];
    await page.routeWebSocket(/\/ws(\?|$)/, (ws) => {
      const server = ws.connectToServer();
      serverSides.push(server);
    });

    await loginInBrowser(page, username);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    const status = page.getByRole("status");
    await expect(status).toHaveText(/^online$/i, { timeout: 10_000 });
    expect(serverSides.length).toBeGreaterThanOrEqual(1);

    // Capture the count before the drop so we can assert a fresh socket was
    // minted post-reconnect (not just the same one bouncing).
    const beforeDrop = serverSides.length;

    // Snapshot the transition log length so we only assert against new
    // entries — the initial connect emits its own connecting/open pair.
    const beforeLen = await page.evaluate(
      () =>
        (window as { __chatd?: { wsTransitions: string[] } }).__chatd?.wsTransitions.length ?? 0,
    );

    // Drop every live server-side handle. The browser sees a real close
    // frame, the api-client's onclose fires, scheduleReconnect kicks in.
    for (const s of serverSides) {
      await s.close();
    }

    // Assert the transient disconnect via the api-client transition log
    // (recorded by main.tsx into window.__chatd.wsTransitions) instead of
    // polling the DOM badge — the badge can be flipped back to "Connected"
    // before Playwright re-reads it on a fast reconnect (#110). 5 s ceiling
    // matches PRD §11 ("Web auto-reconnects within 5 s after a server
    // restart"); a regression in the api-client backoff schedule must trip
    // this assertion (#666).
    await expect
      .poll(
        () =>
          page.evaluate(
            (start: number) =>
              (window as { __chatd?: { wsTransitions: string[] } }).__chatd?.wsTransitions.slice(
                start,
              ) ?? [],
            beforeLen,
          ),
        { timeout: 5_000, message: "expected closed→connecting→open after WS drop" },
      )
      .toEqual(expect.arrayContaining(["closed", "connecting", "open"]));

    expect(serverSides.length).toBeGreaterThan(beforeDrop);

    // Post from a different user AFTER reconnect; the renewed subscription
    // must deliver the live event. (True during-outage replay would need a
    // server-side catchup mechanism — that's a separate issue, not this
    // scenario's responsibility.)
    const body = `post-reconn-${String(Date.now())}`;
    await postViaApi(otherReg.token, channel.id, body);
    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 10_000,
    });
  });
});
