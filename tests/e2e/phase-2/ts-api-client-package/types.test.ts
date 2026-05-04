// AC-2: Exports shared TS types for `User`, `Channel`, `Message`,
// `Event`.
//
// Static side: import the types from the package and run
// `expectTypeOf` assignability checks against representative literals.
// Compile-time failure here means the export shape regressed.
//
// Runtime side: fetch real values via the live server and assert the
// observed JSON shapes match the declared types so type-vs-runtime
// drift is caught as well.

import { describe, it, expect, expectTypeOf } from "vitest";
import type { User, Channel, Message, Event } from "@hackathon/api-client";
import { registerFresh, uniqueChannelName } from "./helpers.js";

describe("AC-2: package exports User / Channel / Message / Event types", () => {
  it("AC-2: User / Channel / Message / Event are assignable from canonical literals (compile-time)", () => {
    const u: User = { id: "u1", username: "alice" };
    expectTypeOf(u).toExtend<User>();
    expectTypeOf<User>().toHaveProperty("id");
    expectTypeOf<User>().toHaveProperty("username");

    const ch: Channel = { id: "c1", name: "general", created_at: "2024-01-01T00:00:00Z" };
    expectTypeOf(ch).toExtend<Channel>();
    expectTypeOf<Channel>().toHaveProperty("id");
    expectTypeOf<Channel>().toHaveProperty("name");
    expectTypeOf<Channel>().toHaveProperty("created_at");

    const m: Message = {
      id: "m1",
      channel_id: "c1",
      sender_user_id: "u1",
      body: "hi",
      created_at: "2024-01-01T00:00:00Z",
    };
    expectTypeOf(m).toExtend<Message>();
    expectTypeOf<Message>().toHaveProperty("body");
    expectTypeOf<Message>().toHaveProperty("channel_id");

    const ev: Event = { type: "message", data: m };
    expectTypeOf(ev).toExtend<Event>();
    // Event must be a discriminated union including "message" and a
    // future "presence" arm — the .type field is at minimum a string.
    expectTypeOf<Event>().toHaveProperty("type");
  });

  it("AC-2: live server returns User shape for me()", async () => {
    const u = await registerFresh("typesuser");
    const me = await u.client.me();
    expect(typeof me.id).toBe("string");
    expect(typeof me.username).toBe("string");
    // No extraneous required fields beyond the declared ones (the
    // declared type allows extras at runtime, but id and username
    // are mandatory).
    expect(me.id.length).toBeGreaterThan(0);
    expect(me.username).toBe(u.username);
  });

  it("AC-2: live server returns Channel + Message shape", async () => {
    const u = await registerFresh("typescm");
    const ch = await u.client.createChannel(uniqueChannelName());
    expect(typeof ch.id).toBe("string");
    expect(typeof ch.name).toBe("string");
    expect(typeof ch.created_at).toBe("string");

    const posted = await u.client.postMessage(ch.id, "ac2-shape-check");
    expect(typeof posted.id).toBe("string");
    expect(posted.channel_id).toBe(ch.id);
    expect(posted.sender_user_id).toBe(u.userId);
    expect(posted.body).toBe("ac2-shape-check");
    expect(typeof posted.created_at).toBe("string");

    const list = await u.client.listMessages(ch.id);
    expect(Array.isArray(list)).toBe(true);
    expect(list.length).toBeGreaterThan(0);
    const found = list.find((m) => m.id === posted.id);
    expect(found).toBeDefined();
    expect(found?.body).toBe("ac2-shape-check");
  });
});
