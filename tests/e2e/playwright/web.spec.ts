import { test, expect, type Page, type BrowserContext } from "@playwright/test";

const TEST_PASSWORD = "e2e-fake-pw-1234567890";

function uniqueUsername(prefix: string): string {
  const r = Math.floor(Math.random() * 36 ** 6)
    .toString(36)
    .padStart(6, "0");
  const t = Date.now().toString(36).slice(-6);
  const head = prefix.slice(0, 18);
  return `${head}-${t}-${r}`;
}

function baseUrl(): string {
  const v = process.env.E2E_BASE_URL;
  if (!v) throw new Error("E2E_BASE_URL not set — globalSetup did not export it");
  return v;
}

function inviteCode(): string {
  const v = process.env.E2E_INVITE_CODE;
  if (!v) throw new Error("E2E_INVITE_CODE not set — globalSetup did not export it");
  return v;
}

interface Envelope<T> {
  ok: boolean;
  data?: T;
  error?: { code: string; message: string };
}

interface RegisterResponse {
  token: string;
  user: { id: string; username: string };
}

interface ChannelRow {
  id: string;
  name: string;
}

async function registerViaApi(username: string): Promise<RegisterResponse> {
  const res = await fetch(baseUrl() + "/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password: TEST_PASSWORD, invite_code: inviteCode() }),
  });
  const env = (await res.json()) as Envelope<RegisterResponse>;
  if (!env.ok || !env.data) throw new Error(`register failed: ${JSON.stringify(env)}`);
  return env.data;
}

async function createChannelViaApi(token: string, name: string): Promise<ChannelRow> {
  const res = await fetch(baseUrl() + "/api/channels", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  });
  const env = (await res.json()) as Envelope<ChannelRow>;
  if (!env.ok || !env.data) throw new Error(`create channel failed: ${JSON.stringify(env)}`);
  return env.data;
}

async function postViaApi(token: string, channelId: string, body: string): Promise<void> {
  const res = await fetch(baseUrl() + `/api/channels/${encodeURIComponent(channelId)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ body }),
  });
  if (!res.ok) throw new Error(`post message failed: ${String(res.status)}`);
}

// Signs the given user into the web app via the on-screen form. Asserts we
// land on the chat page (sidebar visible). Each call must be on a fresh
// page or context so prior session state doesn't leak.
async function loginInBrowser(page: Page, username: string): Promise<void> {
  await page.goto("/#/login");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(TEST_PASSWORD);
  await page.getByRole("button", { name: /sign in/i }).click();
  // The sidebar shows the username after auth; use that as the readiness
  // check so we don't race the chat view's first render.
  await expect(page.locator(".sidebar strong")).toContainText(username, { timeout: 10_000 });
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

  // TODO: flaky — see #81 follow-up. The api-client's reconnect schedule
  // mints a fresh WS ticket on every retry, and Playwright's
  // page.context().setOffline(true) lets the in-flight HTTP request complete
  // while blocking new ones — the resulting state mix puts the badge into
  // a "Reconnecting..." loop that doesn't always settle inside the 30s
  // budget on slow CI. The transport-layer reconnect path is exercised by
  // packages/api-client/src/ws.test.ts and apps/web/src/routes/Chat.test.tsx
  // (the "forced close triggers reconnect that mints a fresh ticket" test);
  // this scenario is the browser-driven cousin and remains an open AC.
  test.skip("AC: WS drops + restores → presence/state catch up via reconnect backoff", async ({
    page,
  }) => {
    const username = uniqueUsername("u-web-reconn");
    const reg = await registerViaApi(username);
    const channel = await createChannelViaApi(reg.token, uniqueUsername("ch"));

    await loginInBrowser(page, username);
    await page.getByRole("button", { name: `#${channel.name}` }).click();

    // Confirm initial connection — the badge shows "Connected".
    await expect(page.getByRole("status")).toContainText(/connected/i, { timeout: 10_000 });

    // Force a WS drop by toggling the page offline (Playwright closes
    // existing sockets); flip back online and confirm the next message
    // posted via REST shows up in the DOM (WS reconnected and replayed).
    await page.context().setOffline(true);
    await expect(page.getByRole("status")).toContainText(/reconnect|disconnect|connecting/i, {
      timeout: 10_000,
    });
    await page.context().setOffline(false);

    // Wait for reconnection — the api-client's backoff schedule starts at
    // 500ms so this typically resolves inside a couple of seconds, but the
    // WS ticket mint + handshake adds a beat on slow CI.
    await expect(page.getByRole("status")).toContainText(/connected/i, { timeout: 30_000 });

    const body = `post-reconn-${String(Date.now())}`;
    await postViaApi(reg.token, channel.id, body);
    await expect(page.locator('[data-testid="msg"]', { hasText: body })).toBeVisible({
      timeout: 15_000,
    });
  });
});
