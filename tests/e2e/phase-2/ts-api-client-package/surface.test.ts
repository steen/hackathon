// AC-1: A TypeScript package at `packages/api-client` exports typed
// functions for the same server endpoints exposed by the Go client.
//
// Strategy: walk the documented surface end-to-end against a real
// server: register -> me -> createChannel -> listChannels ->
// postMessage -> listMessages -> logout. Each step asserts the
// returned shape matches the typed interface (presence of required
// fields). A fixed list of expected method names is asserted against
// the Client prototype so drift with the Go client surface surfaces
// as a one-line diff.

import { describe, it, expect } from "vitest";
import { Client, createClient } from "@hackathon/api-client";
import {
  serverUrl,
  inviteCode,
  uniqueUsername,
  uniqueChannelName,
  strongPassword,
} from "./helpers.js";

const EXPECTED_METHODS = [
  "login",
  "register",
  "me",
  "logout",
  "wsTicket",
  "listChannels",
  "createChannel",
  "listMessages",
  "postMessage",
  "watch",
  "websocket",
] as const;

describe("AC-1: TS api-client exports typed functions for the same endpoints as the Go client", () => {
  it("AC-1: Client surface exposes the documented method set", () => {
    const proto = Client.prototype as unknown as Record<string, unknown>;
    for (const name of EXPECTED_METHODS) {
      expect(typeof proto[name], `Client.${name}`).toBe("function");
    }
  });

  it("AC-1: register -> me -> createChannel -> listChannels -> postMessage -> listMessages -> logout walks end-to-end against a real server", async () => {
    const username = uniqueUsername("alice");
    const password = strongPassword();
    const c = createClient({ baseUrl: serverUrl() });

    const reg = await c.register(username, password, inviteCode());
    expect(typeof reg.token).toBe("string");
    expect(reg.token.length).toBeGreaterThan(0);
    expect(reg.user.username).toBe(username);
    expect(typeof reg.user.id).toBe("string");

    const me = await c.me();
    expect(me.id).toBe(reg.user.id);
    expect(me.username).toBe(username);

    const channelName = uniqueChannelName();
    const created = await c.createChannel(channelName);
    expect(created.name).toBe(channelName);
    expect(typeof created.id).toBe("string");
    expect(typeof created.created_at).toBe("string");

    const channels = await c.listChannels();
    expect(Array.isArray(channels)).toBe(true);
    expect(channels.some((ch) => ch.id === created.id)).toBe(true);

    const body = "hello from AC-1 surface test";
    const posted = await c.postMessage(created.id, body);
    expect(posted.body).toBe(body);
    expect(posted.channel_id).toBe(created.id);
    expect(posted.sender_user_id).toBe(reg.user.id);
    expect(typeof posted.created_at).toBe("string");
    expect(typeof posted.id).toBe("string");

    const messages = await c.listMessages(created.id);
    expect(Array.isArray(messages)).toBe(true);
    expect(messages.some((m) => m.id === posted.id && m.body === body)).toBe(true);

    await c.logout();

    // After logout the in-memory token is cleared; me() should now
    // surface an unauthorized error from the server.
    let unauthAfterLogout = false;
    try {
      await c.me();
    } catch {
      unauthAfterLogout = true;
    }
    expect(unauthAfterLogout).toBe(true);
  });
});
