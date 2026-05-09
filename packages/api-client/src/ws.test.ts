import { describe, expect, it, vi } from "vitest";
import {
  WebSocketClient,
  buildWsUrl,
  decodeFrame,
  watch,
  type WebSocketLike,
  type WSConnectionState,
} from "./ws.js";
import type { ChannelEvent, Event as WsEvent } from "./types.js";

class FakeSocket implements WebSocketLike {
  static instances: FakeSocket[] = [];
  readyState = 0;
  url: string;
  onopen: ((ev: unknown) => void) | null = null;
  onclose: ((ev: { code: number; reason: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }

  open(): void {
    this.readyState = 1;
    this.onopen?.(undefined);
  }
  message(data: unknown): void {
    this.onmessage?.({ data });
  }
  forceClose(code = 1006, reason = "abnormal"): void {
    this.readyState = 3;
    this.onclose?.({ code, reason });
  }
  send(data: string): void {
    this.sent.push(data);
  }
  close(): void {
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: "normal" });
  }
}

function fakeHttp(ticket = "tkt-fake-deadbeef"): {
  wsTicket: () => Promise<{ ticket: string; expires_at: string }>;
  getBaseUrl: () => string;
} {
  return {
    wsTicket: vi.fn(async () => {
      await Promise.resolve();
      return { ticket, expires_at: "2026-01-01T00:00:00Z" };
    }),
    getBaseUrl: () => "http://srv",
  };
}

describe("buildWsUrl", () => {
  it("rewrites http→ws and appends ticket+channel", () => {
    expect(buildWsUrl("http://srv", "abc", "C1")).toBe("ws://srv/ws?ticket=abc&channel=C1");
  });
  it("rewrites https→wss and tolerates trailing slash", () => {
    expect(buildWsUrl("https://srv/", "x")).toBe("wss://srv/ws?ticket=x");
  });
});

describe("decodeFrame", () => {
  it("decodes a typed message frame", () => {
    const f = decodeFrame(JSON.stringify({ type: "message", data: { id: "M1" } }));
    expect(f.type).toBe("message");
    expect((f.data as { id: string }).id).toBe("M1");
  });
  it("returns empty type for non-JSON payloads", () => {
    expect(decodeFrame("not-json").type).toBe("");
  });
  it("preserves presence frames as PresenceEvent shape", () => {
    const f = decodeFrame(
      JSON.stringify({ type: "presence", data: { kind: "join", user_id: "U1" } }),
    );
    expect(f.type).toBe("presence");
  });

  it("round-trips a channel:create frame with the ChannelEvent payload shape", () => {
    const f = decodeFrame(
      JSON.stringify({
        type: "channel",
        data: {
          kind: "create",
          channel: { id: "C1", name: "lobby", created_at: "2026-01-01T00:00:00Z" },
        },
      }),
    );
    expect(f.type).toBe("channel");
    const data = f.data as { kind: string; channel: { id: string; name: string } };
    expect(data.kind).toBe("create");
    expect(data.channel.id).toBe("C1");
    expect(data.channel.name).toBe("lobby");
  });

  it("round-trips a channel:rename frame with the ChannelEvent payload shape", () => {
    const f = decodeFrame(
      JSON.stringify({
        type: "channel",
        data: {
          kind: "rename",
          channel: { id: "C1", name: "renamed", created_at: "2026-01-01T00:00:00Z" },
        },
      }),
    );
    expect(f.type).toBe("channel");
    const data = f.data as { kind: string; channel: { id: string; name: string } };
    expect(data.kind).toBe("rename");
    expect(data.channel.name).toBe("renamed");
  });

  it("ChannelEvent constrains kind to create|rename and carries a Channel", () => {
    const create: ChannelEvent = {
      type: "channel",
      data: {
        kind: "create",
        channel: { id: "C1", name: "lobby", created_at: "2026-01-01T00:00:00Z" },
      },
    };
    const rename: ChannelEvent = {
      type: "channel",
      data: {
        kind: "rename",
        channel: { id: "C1", name: "renamed", created_at: "2026-01-01T00:00:00Z" },
      },
    };
    expect(create.data.kind).toBe("create");
    expect(rename.data.kind).toBe("rename");
    if (rename.data.kind !== "rename" || create.data.kind !== "create") {
      throw new Error("union narrowing failed");
    }
    expect(rename.data.channel.id).toBe(create.data.channel.id);
  });
});

describe("WebSocketClient", () => {
  it("mints a ticket then connects with ?ticket=<hex>&channel=<id>", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp("ticket-hex-fake");
    const c = new WebSocketClient({
      http,
      channelId: "C1",
      WebSocket: FakeSocket,
      reconnect: false,
    });
    await c.connect();
    expect(FakeSocket.instances).toHaveLength(1);
    expect(FakeSocket.instances[0]?.url).toBe("ws://srv/ws?ticket=ticket-hex-fake&channel=C1");
  });

  it("emits typed message events to listeners", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp();
    const c = new WebSocketClient({
      http,
      WebSocket: FakeSocket,
      reconnect: false,
    });
    const messages: WsEvent[] = [];
    c.on("message", (ev) => messages.push(ev));
    await c.connect();
    const sock = FakeSocket.instances[0];
    expect(sock).toBeDefined();
    sock?.open();
    sock?.message(
      JSON.stringify({
        type: "message",
        data: {
          id: "M1",
          channel_id: "C1",
          sender_user_id: "U1",
          body: "hello",
          created_at: "2026-01-01T00:00:00Z",
        },
      }),
    );
    expect(messages).toHaveLength(1);
    expect(messages[0]?.type).toBe("message");
  });

  it("reconnects after a forced close (mints a fresh ticket)", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp("ticket-1");
    let n = 0;
    http.wsTicket = vi.fn(async () => {
      await Promise.resolve();
      n += 1;
      return { ticket: `ticket-${String(n)}`, expires_at: "" };
    });

    const timers: (() => void)[] = [];
    const c = new WebSocketClient({
      http,
      channelId: "C1",
      WebSocket: FakeSocket,
      reconnect: true,
      backoffMs: [1],
      setTimeout: (fn) => {
        timers.push(fn);
        return timers.length;
      },
      clearTimeout: () => undefined,
    });
    await c.connect();
    expect(FakeSocket.instances).toHaveLength(1);
    FakeSocket.instances[0]?.forceClose();
    expect(timers).toHaveLength(1);
    timers[0]?.();
    await new Promise((r) => setImmediate(r));
    expect(FakeSocket.instances).toHaveLength(2);
    expect(FakeSocket.instances[1]?.url).toContain("ticket=ticket-2");
    c.close();
  });

  it("does not reconnect when caller closed", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp();
    let timerSet = false;
    const c = new WebSocketClient({
      http,
      WebSocket: FakeSocket,
      reconnect: true,
      backoffMs: [1],
      setTimeout: () => {
        timerSet = true;
        return 1;
      },
      clearTimeout: () => undefined,
    });
    await c.connect();
    c.close();
    expect(timerSet).toBe(false);
  });

  it("emits transition events: connecting → open → closed → connecting → open across a reconnect", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp("ticket-trans");
    let n = 0;
    http.wsTicket = vi.fn(async () => {
      await Promise.resolve();
      n += 1;
      return { ticket: `ticket-${String(n)}`, expires_at: "" };
    });

    const timers: (() => void)[] = [];
    const c = new WebSocketClient({
      http,
      channelId: "C1",
      WebSocket: FakeSocket,
      reconnect: true,
      backoffMs: [1],
      setTimeout: (fn) => {
        timers.push(fn);
        return timers.length;
      },
      clearTimeout: () => undefined,
    });
    const seen: WSConnectionState[] = [];
    c.on("transition", (s) => seen.push(s));
    await c.connect();
    FakeSocket.instances[0]?.open();
    FakeSocket.instances[0]?.forceClose();
    timers[0]?.();
    await new Promise((r) => setImmediate(r));
    FakeSocket.instances[1]?.open();
    expect(seen).toEqual(["connecting", "open", "closed", "connecting", "open"]);
    c.close();
  });

  it("static observe receives transitions from any instance and disposer stops them", async () => {
    FakeSocket.instances = [];
    const seen: WSConnectionState[] = [];
    const dispose = WebSocketClient.observe((s) => seen.push(s));
    const c = new WebSocketClient({
      http: fakeHttp(),
      WebSocket: FakeSocket,
      reconnect: false,
    });
    await c.connect();
    FakeSocket.instances[0]?.open();
    expect(seen).toEqual(["connecting", "open"]);
    dispose();
    FakeSocket.instances[0]?.forceClose();
    expect(seen).toEqual(["connecting", "open"]);
  });

  it("getState reflects the current connection state", async () => {
    FakeSocket.instances = [];
    const c = new WebSocketClient({
      http: fakeHttp(),
      WebSocket: FakeSocket,
      reconnect: false,
    });
    expect(c.getState()).toBe("closed");
    await c.connect();
    expect(c.getState()).toBe("connecting");
    FakeSocket.instances[0]?.open();
    expect(c.getState()).toBe("open");
    FakeSocket.instances[0]?.forceClose();
    expect(c.getState()).toBe("closed");
  });

  it("send throws before the socket is open", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp();
    const c = new WebSocketClient({
      http,
      WebSocket: FakeSocket,
      reconnect: false,
    });
    await c.connect();
    expect(() => {
      c.send("hi");
    }).toThrow();
  });
});

describe("watch async iterable", () => {
  it("yields message events and stops on close", async () => {
    FakeSocket.instances = [];
    const http = fakeHttp();
    const it = watch(http, "C1", {
      WebSocket: FakeSocket,
    });
    const next1 = it.next();
    await new Promise((r) => setImmediate(r));
    const sock = FakeSocket.instances[0];
    expect(sock).toBeDefined();
    sock?.open();
    sock?.message(JSON.stringify({ type: "message", data: { id: "M1" } }));
    const r1 = await next1;
    expect(r1.done).toBe(false);
    expect((r1.value as WsEvent).type).toBe("message");
    const next2 = it.next();
    sock?.forceClose();
    const r2 = await next2;
    expect(r2.done).toBe(true);
  });
});
