// Phase 8 — Channel create + rename UI. Drives the modal flows end-to-end:
// open from sidebar / header, server validation, success path, conflict
// rendering, and the #general-trigger-hidden invariant.
//
// We bypass `loginInBrowser` and seed the JWT into localStorage instead:
// the per-IP login limiter is Burst=10 in 5 minutes, and the existing
// web.spec.ts + web-mobile.spec.ts already crowd the bucket. Adding 6
// browser logins from this IP tips the whole suite into 429s — see
// aria-log.spec.ts for the same workaround.

import { test, expect, type Page } from "@playwright/test";

import { TOKEN_KEY } from "../../../apps/web/src/api";
import {
  baseUrl,
  createChannelViaApi,
  registerViaApi,
  uniqueUsername,
  waitForChatShell,
} from "./helpers";

async function patchChannel(
  token: string,
  id: string,
  name: string,
): Promise<{ ok: boolean; status: number; body: unknown }> {
  const res = await fetch(baseUrl() + `/api/channels/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  });
  return { ok: res.ok, status: res.status, body: await res.json() };
}

// Seeds the JWT into localStorage on the SPA origin and waits for the
// chat shell. AuthProvider reads TOKEN_KEY on mount, so a full reload
// after the seed is required.
async function seedTokenAndOpen(page: Page, token: string, username: string): Promise<void> {
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

function lowerSlug(prefix: string): string {
  return uniqueUsername(prefix)
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, "");
}

test.describe("Web channel create/rename", () => {
  test("AC: create happy path — sidebar shows the new channel and auto-selects it", async ({
    page,
  }) => {
    const username = uniqueUsername("u-create");
    const reg = await registerViaApi(username);
    await seedTokenAndOpen(page, reg.token, username);

    await page.getByRole("button", { name: /\+ new channel/i }).click();
    const dialog = page.getByRole("dialog", { name: /create channel/i });
    await expect(dialog).toBeVisible();

    const target = lowerSlug("books");
    await dialog.getByLabel(/channel name/i).fill(target);
    await dialog.getByRole("button", { name: /^create$/i }).click();

    await expect(dialog).not.toBeVisible({ timeout: 5_000 });
    // exact: true so the substring lookup doesn't also match the Rename
    // button, whose accessible name is "Rename channel <name>" — without
    // exact, getByRole would resolve both buttons.
    const selected = page.getByRole("button", { name: `#${target}`, exact: true });
    await expect(selected).toBeVisible();
    await expect(selected).toHaveAttribute("aria-current", "true");
  });

  test("AC: create modal — Submit disabled until regex matches; helper text always visible", async ({
    page,
  }) => {
    const username = uniqueUsername("u-create-val");
    const reg = await registerViaApi(username);
    await seedTokenAndOpen(page, reg.token, username);

    await page.getByRole("button", { name: /\+ new channel/i }).click();
    const dialog = page.getByRole("dialog", { name: /create channel/i });
    await expect(dialog).toBeVisible();

    // Helper text is in the DOM from open, not gated on user input.
    await expect(dialog.getByText(/lowercase letters, digits, hyphens/i)).toBeVisible();

    const submit = dialog.getByRole("button", { name: /^create$/i });
    await expect(submit).toBeDisabled();

    // Uppercase is invalid — Submit stays disabled even after typing.
    await dialog.getByLabel(/channel name/i).fill("Books");
    await expect(submit).toBeDisabled();

    // Lowercase + alnum + leading letter — enabled.
    await dialog.getByLabel(/channel name/i).fill("books");
    await expect(submit).toBeEnabled();
  });

  test("AC: create 409 — submitting #general renders the server's conflict message inline", async ({
    page,
  }) => {
    const username = uniqueUsername("u-create-409");
    const reg = await registerViaApi(username);
    await seedTokenAndOpen(page, reg.token, username);

    await page.getByRole("button", { name: /\+ new channel/i }).click();
    const dialog = page.getByRole("dialog", { name: /create channel/i });
    await dialog.getByLabel(/channel name/i).fill("general");
    await dialog.getByRole("button", { name: /^create$/i }).click();

    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("alert")).toBeVisible();
  });

  test("AC: rename trigger is hidden on #general", async ({ page }) => {
    const username = uniqueUsername("u-rename-gen");
    const reg = await registerViaApi(username);
    await seedTokenAndOpen(page, reg.token, username);

    await page.getByRole("button", { name: "#general", exact: true }).click();
    await expect(page.getByRole("button", { name: /^rename$/i })).toHaveCount(0);
    await expect(page.getByRole("button", { name: /^rename channel /i })).toHaveCount(0);
  });

  test("AC: rename happy path — header + sidebar update locally", async ({ page }) => {
    const username = uniqueUsername("u-rename");
    const reg = await registerViaApi(username);
    const original = lowerSlug("ren");
    const channel = await createChannelViaApi(reg.token, original);

    await seedTokenAndOpen(page, reg.token, username);
    await page.getByRole("button", { name: `#${channel.name}`, exact: true }).click();

    const rename = page.getByRole("button", {
      name: new RegExp(`^Rename channel ${channel.name}$`, "i"),
    });
    await expect(rename).toBeVisible();
    await rename.click();

    const dialog = page.getByRole("dialog", { name: /rename channel/i });
    await expect(dialog).toBeVisible();
    const input = dialog.getByLabel(/new name/i);
    await expect(input).toHaveValue(channel.name);

    const target = lowerSlug("rd");
    await input.fill(target);
    await dialog.getByRole("button", { name: /^rename$/i }).click();

    await expect(dialog).not.toBeVisible({ timeout: 5_000 });
    await expect(page.getByRole("button", { name: `#${target}`, exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { level: 2, name: target })).toBeVisible();
  });

  test("AC: rename 409 — targeting #general surfaces the server message inline", async ({
    page,
  }) => {
    const username = uniqueUsername("u-rename-409");
    const reg = await registerViaApi(username);
    const original = lowerSlug("rn");
    const channel = await createChannelViaApi(reg.token, original);

    // Pre-flight: confirm the server actually returns a non-2xx for
    // "rename to general" so the test fails for the right reason if
    // server semantics ever drift.
    const probe = await patchChannel(reg.token, channel.id, "general");
    expect(probe.ok).toBe(false);

    await seedTokenAndOpen(page, reg.token, username);
    await page.getByRole("button", { name: `#${channel.name}`, exact: true }).click();
    await page
      .getByRole("button", { name: new RegExp(`^Rename channel ${channel.name}$`, "i") })
      .click();

    const dialog = page.getByRole("dialog", { name: /rename channel/i });
    const input = dialog.getByLabel(/new name/i);
    await input.fill("general");
    await dialog.getByRole("button", { name: /^rename$/i }).click();

    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("alert")).toBeVisible();
  });
});
