// AC-5: Provides a WebSocketClient class with an event emitter API
// and reconnect support.
//
// Asserts that `'open'` and `'message'` fire on a real connection,
// then force-closes the underlying socket, posts another REST
// message, and asserts a second `'open'` plus the new `'message'`
// arrive (reconnect happened). Also asserts `'close'` fires once
// between the two `'open'` events.

import { describe, it, expect } from "vitest";
import { type Event as WsEvent, type Message } from "@hackathon/api-client";
import { registerFresh, uniqueChannelName } from "./helpers.js";

interface Deferred {
  promise: Promise<true>;
  resolve: () => void;
  reject: (e: unknown) => void;
}
function deferred(): Deferred {
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

describe("AC-5: WebSocketClient exposes event emitter API + reconnects", () => {
  it("AC-5: emits 'open' + 'message' on a live connection, then 'close' + 'open' after a force-close, and delivers a follow-up message", async () => {
    const u = await registerFresh("wsem");
    const ch = await u.client.createChannel(uniqueChannelName());

    const ws = u.client.websocket(ch.id);

    let openCount = 0;
    let closeCount = 0;
    const messages: WsEvent[] = [];
    const firstOpen = deferred();
    const secondOpen = deferred();
    ws.on("open", () => {
      openCount += 1;
      if (openCount === 1) firstOpen.resolve();
      else if (openCount === 2) secondOpen.resolve();
    });
    ws.on("close", () => {
      closeCount += 1;
    });
    ws.on("message", (ev) => {
      messages.push(ev);
    });

    await ws.connect();
    await Promise.race([firstOpen.promise, timeoutReject("first open timeout", 5000)]);

    // Post a message via REST and wait for it to arrive on the
    // emitter as a 'message' event of type "message".
    const firstBody = "ac5-first-" + Math.random().toString(36).slice(2, 8);
    const firstWait = waitForMessage(messages, firstBody);
    await u.client.postMessage(ch.id, firstBody);
    await Promise.race([firstWait, timeoutReject("first message timeout", 5000)]);

    // Force-close the underlying socket. The client is configured
    // to reconnect by default. We do not call ws.close() because
    // that would set `closed=true` and disable reconnect.
    const inner = (ws as unknown as { ws: { close: (c?: number) => void } | null }).ws;
    expect(inner).not.toBeNull();
    if (inner === null) throw new Error("ws inner is null");
    // Browser WebSocket close codes: 1000 or 3000-4999 only.
    // 4000 = app-defined "force close" — surfaces to onclose, which
    // schedules reconnect because the client did not call close()
    // on the WebSocketClient itself (closed flag stays false).
    inner.close(4000);

    await Promise.race([secondOpen.promise, timeoutReject("second open timeout", 8000)]);
    expect(closeCount).toBeGreaterThanOrEqual(1);

    const secondBody = "ac5-second-" + Math.random().toString(36).slice(2, 8);
    const secondWait = waitForMessage(messages, secondBody);
    await u.client.postMessage(ch.id, secondBody);
    await Promise.race([secondWait, timeoutReject("second message timeout", 5000)]);

    expect(openCount).toBeGreaterThanOrEqual(2);

    ws.close();
  });
});

function timeoutReject(reason: string, ms: number): Promise<never> {
  return new Promise<never>((_, rej) => {
    setTimeout(() => {
      rej(new Error(reason));
    }, ms);
  });
}

function waitForMessage(buf: WsEvent[], body: string): Promise<WsEvent> {
  return new Promise<WsEvent>((resolve, reject) => {
    const start = Date.now();
    const tick = (): void => {
      const found = buf.find(
        (ev) => ev.type === "message" && (ev.data as Message | undefined)?.body === body,
      );
      if (found) {
        resolve(found);
        return;
      }
      if (Date.now() - start > 6000) {
        reject(new Error(`message with body=${body} did not arrive`));
        return;
      }
      setTimeout(tick, 50);
    };
    tick();
  });
}
