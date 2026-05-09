import { randomBytes } from "node:crypto";
import { WebSocket as NodeWebSocket } from "ws";
import { createClient, type Client, type WebSocketCtor } from "@hackathon/api-client";

// Node has no global WebSocket on the LTS used in CI; the api-client
// falls back to globalThis.WebSocket when no WebSocket ctor is passed.
// Thread the `ws` package's WebSocket through every helper-built Client
// so tests run identically on dev (where node may expose one) and CI.
// Cast through `unknown` because the `ws` package types its `WebSocket`
// against the DOM lib's stricter `onopen: (ev: Event) => void`, while
// api-client's WebSocketLike uses `(ev: unknown) => void` for runtime
// portability. The shapes match at runtime.
const WSCtor = NodeWebSocket as unknown as WebSocketCtor;

export function serverUrl(): string {
  const v = process.env.E2E_SERVER_URL;
  if (!v) throw new Error("E2E_SERVER_URL missing — globalSetup did not run");
  return v;
}

export function inviteCode(): string {
  const v = process.env.E2E_INVITE_CODE;
  if (!v) throw new Error("E2E_INVITE_CODE missing — globalSetup did not run");
  return v;
}

// Per-test usernames must satisfy server regex ^[a-z0-9_-]{3,32}$
// (L37 lowercase-only). Hex from randomBytes is in [0-9a-f]. Prefix is
// fixed-ascii lowercase.
export function uniqueUsername(prefix = "u"): string {
  return `${prefix}-${randomBytes(6).toString("hex")}`;
}

export function uniqueChannelName(prefix = "c"): string {
  return `${prefix}-${randomBytes(5).toString("hex")}`;
}

export function strongPassword(): string {
  // Server min is 10 chars; 32 hex is well above it. Random per-test.
  return randomBytes(16).toString("hex");
}

export interface RegisteredUser {
  client: Client;
  username: string;
  password: string;
  token: string;
  userId: string;
}

// Register a fresh user against the running server. Token is held in
// the client's default in-memory slot, so subsequent calls on the
// returned client are authenticated.
export async function registerFresh(prefix = "u"): Promise<RegisteredUser> {
  const username = uniqueUsername(prefix);
  const password = strongPassword();
  const client = createClient({ baseUrl: serverUrl(), WebSocket: WSCtor });
  const auth = await client.register(username, password, inviteCode());
  return { client, username, password, token: auth.token, userId: auth.user.id };
}
