// Phase-10 wire-type drift assertions (decision-log L10 + L26).
//
// This suite is the TS+Go cross-language guard for the Phase-10
// encrypted-message wire types: User pubkeys, Channel.is_public, the
// MessageEnvelope shape, the WrapEntry / MembershipBlock shapes, and the
// extended ChannelEventKind union (members_changed, key_received,
// wrap_failed). The PR that introduces any of those types adds the
// matching assertion here so a one-sided edit fails CI before a server
// PR (#4..#9) tries to consume the type.
//
// The TS-side checks are compile-time `expectTypeOf` assertions plus a
// runtime literal that exercises the discriminated-union narrowing. The
// Go-side checks read the goclient sources directly and assert that
// every new JSON tag is present byte-for-byte; structural-text checks
// avoid pulling Go into the JS test harness.

import { describe, it, expect, expectTypeOf } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type {
  User,
  Channel,
  Message,
  DMMessage,
  MessageEnvelope,
  WrapEntry,
  MembershipBlock,
  ChannelEvent,
  ChannelEventKind,
  ChannelEventData,
} from "@hackathon/api-client";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const goClientDir = resolve(repoRoot, "packages", "go-client");

function readGo(file: string): string {
  return readFileSync(resolve(goClientDir, file), "utf8");
}

describe("Phase-10 wire types — TS surface", () => {
  it("User has optional box_pubkey and sign_pubkey", () => {
    const u: User = {
      id: "u1",
      username: "alice",
      box_pubkey: "AAAA",
      sign_pubkey: "BBBB",
    };
    expectTypeOf(u).toExtend<User>();
    expectTypeOf<User>().toHaveProperty("box_pubkey");
    expectTypeOf<User>().toHaveProperty("sign_pubkey");
    // optional-first per L26: a User without pubkeys is still valid until
    // #4 lands the server populator.
    const bare: User = { id: "u1", username: "alice" };
    expectTypeOf(bare).toExtend<User>();
  });

  it("Channel has optional is_public", () => {
    const ch: Channel = {
      id: "c1",
      name: "general",
      created_at: "2026-05-09T00:00:00Z",
      is_public: true,
    };
    expectTypeOf(ch).toExtend<Channel>();
    expectTypeOf<Channel>().toHaveProperty("is_public");
  });

  it("MessageEnvelope shape matches L21", () => {
    const env: MessageEnvelope = {
      cipher_suite: 1,
      key_generation_id: 1,
      nonce: "AAAA",
      ciphertext: "BBBB",
      sender_sign_pubkey: "CCCC",
      signature: "DDDD",
      client_created_at: "2026-05-09T00:00:00Z",
    };
    expectTypeOf(env).toExtend<MessageEnvelope>();
    expectTypeOf<MessageEnvelope>().toHaveProperty("cipher_suite");
    expectTypeOf<MessageEnvelope>().toHaveProperty("key_generation_id");
    expectTypeOf<MessageEnvelope>().toHaveProperty("nonce");
    expectTypeOf<MessageEnvelope>().toHaveProperty("ciphertext");
    expectTypeOf<MessageEnvelope>().toHaveProperty("sender_sign_pubkey");
    expectTypeOf<MessageEnvelope>().toHaveProperty("signature");
    expectTypeOf<MessageEnvelope>().toHaveProperty("client_created_at");
  });

  it("WrapEntry shape matches L5 (recipient_user_id is optional)", () => {
    const w: WrapEntry = {
      wrapped_key: "AAAA",
      sender_box_pubkey: "BBBB",
      nonce: "CCCC",
    };
    expectTypeOf(w).toExtend<WrapEntry>();
    const w2: WrapEntry = {
      recipient_user_id: "u1",
      wrapped_key: "AAAA",
      sender_box_pubkey: "BBBB",
      nonce: "CCCC",
    };
    expectTypeOf(w2).toExtend<WrapEntry>();
  });

  it("MembershipBlock shape matches §10 (inviter_signature is nullable)", () => {
    const b: MembershipBlock = {
      inviter_user_id: "u1",
      inviter_sign_pubkey: "AAAA",
      invitee_box_pubkey: "BBBB",
      invitee_sign_pubkey: "CCCC",
      added_at: "2026-05-09T00:00:00Z",
      inviter_signature: "DDDD",
    };
    expectTypeOf(b).toExtend<MembershipBlock>();
    // Public-channel auto-add carve-out: inviter_signature is null.
    const pub: MembershipBlock = {
      inviter_user_id: "u1",
      inviter_sign_pubkey: "AAAA",
      invitee_box_pubkey: "BBBB",
      invitee_sign_pubkey: "CCCC",
      added_at: "2026-05-09T00:00:00Z",
      inviter_signature: null,
    };
    expectTypeOf(pub).toExtend<MembershipBlock>();
  });

  it("Message and DMMessage carry optional envelope", () => {
    const env: MessageEnvelope = {
      cipher_suite: 1,
      key_generation_id: 1,
      nonce: "AAAA",
      ciphertext: "BBBB",
      sender_sign_pubkey: "CCCC",
      signature: "DDDD",
      client_created_at: "2026-05-09T00:00:00Z",
    };
    const m: Message = {
      id: "m1",
      channel_id: "c1",
      sender_user_id: "u1",
      body: "hi",
      created_at: "2026-05-09T00:00:00Z",
      envelope: env,
    };
    expectTypeOf(m).toExtend<Message>();
    expectTypeOf<Message>().toHaveProperty("envelope");

    const dm: DMMessage = {
      id: "dm1",
      conversation_id: "conv1",
      sender_user_id: "u1",
      body: "hi",
      created_at: "2026-05-09T00:00:00Z",
      envelope: env,
    };
    expectTypeOf(dm).toExtend<DMMessage>();
    expectTypeOf<DMMessage>().toHaveProperty("envelope");
  });

  it("ChannelEventKind covers create/rename + Phase-10 extensions", () => {
    const kinds: ChannelEventKind[] = [
      "create",
      "rename",
      "members_changed",
      "key_received",
      "wrap_failed",
    ];
    expect(kinds).toHaveLength(5);
  });

  it("ChannelEvent discriminated union narrows by kind", () => {
    const ch: Channel = {
      id: "c1",
      name: "general",
      created_at: "2026-05-09T00:00:00Z",
    };
    const u: User = { id: "u1", username: "alice" };

    const create: ChannelEvent = { type: "channel", data: { kind: "create", channel: ch } };
    const rename: ChannelEvent = { type: "channel", data: { kind: "rename", channel: ch } };
    const membersChanged: ChannelEvent = {
      type: "channel",
      data: {
        kind: "members_changed",
        channel_id: "c1",
        current_generation_id: 1,
        members_at_rotation: [u],
      },
    };
    const keyReceived: ChannelEvent = {
      type: "channel",
      data: { kind: "key_received", channel_id: "c1", generation_id: 1 },
    };
    const wrapFailed: ChannelEvent = {
      type: "channel",
      data: { kind: "wrap_failed", channel_id: "c1", generation_id: 1 },
    };

    const events = [create, rename, membersChanged, keyReceived, wrapFailed];
    for (const e of events) {
      expect(e.type).toBe("channel");
    }

    // Discriminated-union narrowing — proves the new variants are usable
    // without `as` casts. `data: ChannelEventData` is the union.
    const handle = (d: ChannelEventData): string => {
      switch (d.kind) {
        case "create":
        case "rename":
          return d.channel.id;
        case "members_changed":
          return `${d.channel_id}:${String(d.current_generation_id)}:${String(d.members_at_rotation.length)}`;
        case "key_received":
        case "wrap_failed":
          return `${d.channel_id}:${String(d.generation_id)}`;
      }
    };
    expect(handle(create.data)).toBe("c1");
    expect(handle(membersChanged.data)).toBe("c1:1:1");
    expect(handle(wrapFailed.data)).toBe("c1:1");
  });
});

describe("Phase-10 wire types — Go-side mirror (drift guard, L10)", () => {
  // Each block reads the goclient file once, then asserts the JSON tags
  // exist byte-for-byte. The matching TS-side fields above already
  // assert the tag names indirectly through the property literals;
  // these checks fail if a future PR edits one side without the other.
  it("auth.go::User mirrors box_pubkey and sign_pubkey", () => {
    const src = readGo("auth.go");
    expect(src).toContain('`json:"box_pubkey,omitempty"`');
    expect(src).toContain('`json:"sign_pubkey,omitempty"`');
  });

  it("users.go::UserSummary mirrors box_pubkey and sign_pubkey", () => {
    const src = readGo("users.go");
    expect(src).toContain('`json:"box_pubkey,omitempty"`');
    expect(src).toContain('`json:"sign_pubkey,omitempty"`');
  });

  it("channels.go::Channel mirrors is_public", () => {
    const src = readGo("channels.go");
    expect(src).toContain('`json:"is_public,omitempty"`');
  });

  it("messages.go declares MessageEnvelope, WrapEntry, MembershipBlock", () => {
    const src = readGo("messages.go");
    expect(src).toContain("type MessageEnvelope struct");
    expect(src).toContain('`json:"cipher_suite"`');
    expect(src).toContain('`json:"key_generation_id"`');
    expect(src).toContain('`json:"nonce"`');
    expect(src).toContain('`json:"ciphertext"`');
    expect(src).toContain('`json:"sender_sign_pubkey"`');
    expect(src).toContain('`json:"signature"`');
    expect(src).toContain('`json:"client_created_at"`');

    expect(src).toContain("type WrapEntry struct");
    expect(src).toContain('`json:"recipient_user_id,omitempty"`');
    expect(src).toContain('`json:"wrapped_key"`');
    expect(src).toContain('`json:"sender_box_pubkey"`');

    expect(src).toContain("type MembershipBlock struct");
    expect(src).toContain('`json:"inviter_user_id"`');
    expect(src).toContain('`json:"inviter_sign_pubkey"`');
    expect(src).toContain('`json:"invitee_box_pubkey"`');
    expect(src).toContain('`json:"invitee_sign_pubkey"`');
    expect(src).toContain('`json:"added_at"`');
    expect(src).toContain('`json:"inviter_signature"`');

    // Message.Envelope is the optional pointer.
    expect(src).toMatch(/Envelope\s+\*MessageEnvelope\s+`json:"envelope,omitempty"`/);
  });

  it("dms.go::DMMessage mirrors envelope as optional pointer", () => {
    const src = readGo("dms.go");
    expect(src).toMatch(/Envelope\s+\*MessageEnvelope\s+`json:"envelope,omitempty"`/);
  });

  it("ws.go::ChannelEvent declares Phase-10 kind constants and union fields", () => {
    const src = readGo("ws.go");
    // gofmt collapses const-block alignment to a single padding column,
    // so match the post-gofmt single-space form.
    expect(src).toMatch(/ChannelEventKindMembersChanged\s+=\s+"members_changed"/);
    expect(src).toMatch(/ChannelEventKindKeyReceived\s+=\s+"key_received"/);
    expect(src).toMatch(/ChannelEventKindWrapFailed\s+=\s+"wrap_failed"/);
    expect(src).toContain('`json:"channel_id,omitempty"`');
    expect(src).toContain('`json:"current_generation_id,omitempty"`');
    expect(src).toContain('`json:"generation_id,omitempty"`');
    expect(src).toContain('`json:"members_at_rotation,omitempty"`');
  });
});
