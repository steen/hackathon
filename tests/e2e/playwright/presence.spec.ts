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

import { test, expect, type BrowserContext } from "@playwright/test";

import { createChannelViaApi, loginInBrowser, registerViaApi, uniqueUsername } from "./helpers";

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

      // Alice clicks into the channel so the Chat view (which hosts the
      // presence panel) renders. Bob only needs an open WS connection for
      // presence to fire — presence is hub-global, ref-counted on userID,
      // not channel-scoped (see apps/server/internal/hub/), so bob does not
      // need to enter any channel.
      await loginInBrowser(pageAlice, aliceName);
      await pageAlice.getByRole("button", { name: `#${channel.name}` }).click();

      await loginInBrowser(pageBob, bobName);

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
