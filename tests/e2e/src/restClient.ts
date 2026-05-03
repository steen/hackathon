// Thin REST helper used by scenarios that need to drive the server directly
// (channel creation, presence probe, second-client posts). The CLI doesn't
// expose a `create-channel` subcommand — channel creation is a REST-only path
// that the smoke harness already drives this way.

export interface RegisterResponse {
  token: string;
  user: { id: string; username: string };
}

export interface ChannelRow {
  id: string;
  name: string;
  created_at: string;
}

export interface MessageRow {
  id: string;
  channel_id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
}

interface Envelope<T> {
  ok: boolean;
  data?: T;
  error?: { code: string; message: string };
}

async function decode<T>(res: Response, op: string): Promise<T> {
  const text = await res.text();
  let parsed: Envelope<T>;
  try {
    parsed = JSON.parse(text) as Envelope<T>;
  } catch {
    throw new Error(
      `${op}: non-JSON response (status ${String(res.status)}): ${text.slice(0, 200)}`,
    );
  }
  if (!parsed.ok) {
    throw new Error(
      `${op} failed: status=${String(res.status)} code=${parsed.error?.code ?? "?"} msg=${parsed.error?.message ?? "?"}`,
    );
  }
  return parsed.data as T;
}

export async function registerUser(
  baseUrl: string,
  inviteCode: string,
  username: string,
  password: string,
): Promise<RegisterResponse> {
  const res = await fetch(baseUrl + "/api/auth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password, invite_code: inviteCode }),
  });
  return decode<RegisterResponse>(res, "register");
}

export async function loginUser(
  baseUrl: string,
  username: string,
  password: string,
): Promise<RegisterResponse> {
  const res = await fetch(baseUrl + "/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  return decode<RegisterResponse>(res, "login");
}

export async function createChannel(
  baseUrl: string,
  token: string,
  name: string,
): Promise<ChannelRow> {
  const res = await fetch(baseUrl + "/api/channels", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name }),
  });
  return decode<ChannelRow>(res, "create channel");
}

export async function postMessage(
  baseUrl: string,
  token: string,
  channelId: string,
  body: string,
): Promise<MessageRow> {
  const res = await fetch(baseUrl + `/api/channels/${encodeURIComponent(channelId)}/messages`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ body }),
  });
  return decode<MessageRow>(res, "post message");
}

export async function listPresence(
  baseUrl: string,
  token: string,
): Promise<{ users: { id: string; username: string }[] }> {
  const res = await fetch(baseUrl + "/api/presence", {
    headers: { Authorization: `Bearer ${token}` },
  });
  return decode<{ users: { id: string; username: string }[] }>(res, "presence list");
}

export async function debugSubsCount(baseUrl: string, channel: string): Promise<number> {
  const res = await fetch(baseUrl + `/debug/subs?channel=${encodeURIComponent(channel)}`);
  const text = await res.text();
  const n = Number.parseInt(text.trim(), 10);
  if (Number.isNaN(n)) throw new Error(`debug/subs returned non-number: ${text}`);
  return n;
}

// Server validates usernames as 3-32 chars matching [letters, digits, dash,
// underscore]. Keep the prefix short and use a base-36 tail for uniqueness
// across concurrent runs. 6 base-36 chars ≈ 2^31 combos — collision-safe
// inside a single CI job.
export function uniqueUsername(prefix: string): string {
  const r = Math.floor(Math.random() * 36 ** 6)
    .toString(36)
    .padStart(6, "0");
  const t = Date.now().toString(36).slice(-6);
  // prefix is caller-controlled; cap to keep total ≤ 32.
  const head = prefix.slice(0, 18);
  return `${head}-${t}-${r}`;
}
