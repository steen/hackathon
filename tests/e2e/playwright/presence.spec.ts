// Covers AC-4 (web half) of specs/plans/phase-2/50-feature-presence.md:
// "The web app shows online users in the chat page".
//
// The CLI half of AC-4 is covered separately under tests/e2e/phase-2/presence/
// (per specs/test-analysis/phase-2/presence.md). This spec asserts only the
// browser-rendered online-users panel, since the agent-owned Go E2E suite is
// out of footprint for browser-driven assertions.
//
// Strategy: drive two real browser contexts (alice + bob) the same way
// web.spec.ts does for the cross-receive test. Alice's chat page renders a
// `[data-testid="presence-list"]` with one `[data-testid="presence-user-<id>"]`
// per online user (see apps/web/src/routes/Chat.tsx). Asserting by user id
// avoids the seed-vs-live-join username-blank discrepancy in
// apps/web/src/hooks/usePresence.ts (live `presence` WS frames omit username).

import { test, expect, type BrowserContext, type Page } from "@playwright/test";

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

async function loginInBrowser(page: Page, username: string): Promise<void> {
  await page.goto("/#/login");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(TEST_PASSWORD);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page.locator(".sidebar strong")).toContainText(username, { timeout: 10_000 });
}

test.describe("Web presence panel (AC-4 web half)", () => {
  test("AC: alice sees bob in online-users panel; panel updates when bob's context closes", async ({
    browser,
  }) => {
    const aliceName = uniqueUsername("u-pres-A");
    const bobName = uniqueUsername("u-pres-B");
    const alice = await registerViaApi(aliceName);
    const bob = await registerViaApi(bobName);
    const channel = await createChannelViaApi(alice.token, uniqueUsername("ch"));

    const ctxAlice: BrowserContext = await browser.newContext();
    const ctxBob: BrowserContext = await browser.newContext();
    try {
      const pageAlice = await ctxAlice.newPage();
      const pageBob = await ctxBob.newPage();

      // Order matters for which path puts each user into the other's panel:
      // alice loads first (panel may be empty or list herself only); bob
      // logs in next, which fires a `presence` join event alice's WS picks up.
      await loginInBrowser(pageAlice, aliceName);
      await pageAlice.getByRole("button", { name: `#${channel.name}` }).click();

      await loginInBrowser(pageBob, bobName);
      await pageBob.getByRole("button", { name: `#${channel.name}` }).click();

      // Asserts bob shows up in alice's panel. Selecting by id-suffixed
      // testid sidesteps the username-blank shape of live join frames
      // (usePresence.ts enriches only seed entries from /api/presence).
      const presenceList = pageAlice.locator('[data-testid="presence-list"]');
      const bobEntry = presenceList.locator(`[data-testid="presence-user-${bob.user.id}"]`);
      await expect(bobEntry).toBeVisible({ timeout: 10_000 });

      // Tear down bob's browser context. The server's per-user ref-counted
      // hub fires a `presence` leave once both of bob's WS connections (the
      // messages WS + the dedicated presence WS in usePresence) are gone.
      await ctxBob.close();

      await expect(bobEntry).toHaveCount(0, { timeout: 10_000 });
    } finally {
      // ctxBob is already closed in the happy path; close() is idempotent
      // enough that a double-close on failure paths is fine.
      await ctxAlice.close();
      await ctxBob.close().catch(() => {
        /* already closed */
      });
    }
  });
});
