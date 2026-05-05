// Shared Playwright helpers for the web e2e suite. Imported by every
// *.spec.ts under this directory so a fix to register/login/channel-create
// flow lands once instead of drifting across copies (#642).

import { expect, type Page } from "@playwright/test";

export const TEST_PASSWORD = "e2e-fake-pw-1234567890";

export function uniqueUsername(prefix: string): string {
  const r = Math.floor(Math.random() * 36 ** 6)
    .toString(36)
    .padStart(6, "0");
  const t = Date.now().toString(36).slice(-6);
  const head = prefix.slice(0, 18);
  return `${head}-${t}-${r}`;
}

export function baseUrl(): string {
  const v = process.env.E2E_BASE_URL;
  if (!v) throw new Error("E2E_BASE_URL not set — globalSetup did not export it");
  return v;
}

export function inviteCode(): string {
  const v = process.env.E2E_INVITE_CODE;
  if (!v) throw new Error("E2E_INVITE_CODE not set — globalSetup did not export it");
  return v;
}

export interface Envelope<T> {
  ok: boolean;
  data?: T;
  error?: { code: string; message: string };
}

export interface RegisterResponse {
  token: string;
  user: { id: string; username: string };
}

export interface ChannelRow {
  id: string;
  name: string;
}

export async function registerViaApi(username: string): Promise<RegisterResponse> {
  const res = await fetch(baseUrl() + "/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password: TEST_PASSWORD, invite_code: inviteCode() }),
  });
  const env = (await res.json()) as Envelope<RegisterResponse>;
  if (!env.ok || !env.data) throw new Error(`register failed: ${JSON.stringify(env)}`);
  return env.data;
}

export async function createChannelViaApi(token: string, name: string): Promise<ChannelRow> {
  const res = await fetch(baseUrl() + "/api/channels", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  });
  const env = (await res.json()) as Envelope<ChannelRow>;
  if (!env.ok || !env.data) throw new Error(`create channel failed: ${JSON.stringify(env)}`);
  return env.data;
}

// Signs the given user into the web app via the on-screen form. Asserts we
// land on the chat page (sidebar visible). Each call must be on a fresh
// page or context so prior session state doesn't leak.
export async function loginInBrowser(page: Page, username: string): Promise<void> {
  await page.goto("/#/login");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password").fill(TEST_PASSWORD);
  await page.getByRole("button", { name: /sign in/i }).click();
  // The sidebar shows the username after auth; use that as the readiness
  // check so we don't race the chat view's first render.
  await expect(page.locator(".sidebar strong")).toContainText(username, { timeout: 10_000 });
}
