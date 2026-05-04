// AC-6: Once 50-feature-presence.md lands, the client must surface
// the `presence` event type (kind `join` / `leave`) through the same
// emitter; design the `Event` union with that in mind.
//
// Server-side presence is wired in apps/server/internal/wsapi/handler.go
// (BroadcastAll(presenceFrame(...)) on first connect / last disconnect)
// and apps/server/internal/wsapi/presence.go defines the
// `{type:"presence", data:{kind, user_id}}` envelope. The TS client
// already includes `PresenceEvent` in its Event union (see
// packages/api-client/src/types.ts).
//
// This test connects two distinct users via WebSocketClient and
// asserts the watcher (user A) receives `kind:"join"` when user B
// connects and `kind:"leave"` when user B disconnects.

import { describe, it, expect } from "vitest";
import { type Event as WsEvent, type PresenceEvent } from "@hackathon/api-client";
import { registerFresh, uniqueChannelName } from "./helpers.js";

interface Signal {
  promise: Promise<true>;
  resolve: () => void;
  reject: (e: unknown) => void;
}

function signal(): Signal {
  let resolve!: () => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<true>((res, rej) => {
    resolve = (): void => {
      res(true);
    };
    reject = rej;
  });
  return { promise, resolve, reject };
}

interface Holder<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (e: unknown) => void;
}

function holder<T>(): Holder<T> {
  let resolve!: (value: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function timeoutReject(reason: string, ms: number): Promise<never> {
  return new Promise<never>((_, rej) => {
    setTimeout(() => {
      rej(new Error(reason));
    }, ms);
  });
}

function isPresenceFor(ev: WsEvent, kind: "join" | "leave", userId: string): ev is PresenceEvent {
  if (ev.type !== "presence") return false;
  const data = ev.data as PresenceEvent["data"] | undefined;
  if (!data) return false;
  return data.kind === kind && data.user_id === userId;
}

describe("AC-6: presence event surfaced through the emitter", () => {
  it("AC-6: WebSocketClient receives presence join/leave events when a peer connects/disconnects", async () => {
    const watcher = await registerFresh("ac6w");
    const peer = await registerFresh("ac6p");
    const ch = await watcher.client.createChannel(uniqueChannelName("ac6"));

    // Both clients subscribe to the same channel. Presence is
    // BroadcastAll on the server, so any open connection sees the
    // join/leave; scoping to a shared channel keeps the assertion
    // narrow against future server behaviour changes.
    const wsA = watcher.client.websocket(ch.id);
    const joinSeen = holder<PresenceEvent>();
    const leaveSeen = holder<PresenceEvent>();
    const aOpen = signal();

    wsA.on("open", () => {
      aOpen.resolve();
    });
    wsA.on("message", (ev) => {
      if (isPresenceFor(ev, "join", peer.userId)) {
        joinSeen.resolve(ev);
        return;
      }
      if (isPresenceFor(ev, "leave", peer.userId)) {
        leaveSeen.resolve(ev);
      }
    });

    await wsA.connect();
    await Promise.race([aOpen.promise, timeoutReject("watcher open timeout", 5000)]);

    // Open the peer's WS in a second step so the join broadcast is
    // observed by the already-open watcher. With reconnect disabled,
    // wsB.close() below produces a deterministic last-disconnect.
    const wsB = peer.client.websocket(ch.id);
    const bOpen = signal();
    wsB.on("open", () => {
      bOpen.resolve();
    });

    await wsB.connect();
    await Promise.race([bOpen.promise, timeoutReject("peer open timeout", 5000)]);

    const joinEv = await Promise.race([
      joinSeen.promise,
      timeoutReject("presence join not received", 5000),
    ]);
    expect(joinEv.type).toBe("presence");
    expect(joinEv.data.kind).toBe("join");
    expect(joinEv.data.user_id).toBe(peer.userId);

    wsB.close();

    const leaveEv = await Promise.race([
      leaveSeen.promise,
      timeoutReject("presence leave not received", 5000),
    ]);
    expect(leaveEv.type).toBe("presence");
    expect(leaveEv.data.kind).toBe("leave");
    expect(leaveEv.data.user_id).toBe(peer.userId);

    wsA.close();
  });
});
