import type { HttpClient } from "./http.js";
import type { Event as WsEvent, WSTicket } from "./types.js";

export type WSReadyState = 0 | 1 | 2 | 3;

export interface WebSocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((ev: unknown) => void) | null;
  onclose: ((ev: { code: number; reason: string }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
}

export type WebSocketCtor = new (url: string) => WebSocketLike;

export type WSEventName = "open" | "close" | "message" | "error";

type Listener<T> = (arg: T) => void;

interface Listeners {
  open: Listener<void>[];
  close: Listener<{ code: number; reason: string }>[];
  message: Listener<WsEvent>[];
  error: Listener<unknown>[];
}

export interface WebSocketClientOptions {
  http: Pick<HttpClient, "wsTicket" | "getBaseUrl">;
  channelId?: string;
  WebSocket?: WebSocketCtor;
  reconnect?: boolean;
  // Backoff schedule in ms. Index clamps at length-1 once exhausted.
  backoffMs?: number[];
  // Reconnect can run in tests with deterministic delays by injecting
  // setTimeout/clearTimeout. Defaults to globals.
  setTimeout?: (fn: () => void, ms: number) => unknown;
  clearTimeout?: (handle: unknown) => void;
}

const DEFAULT_BACKOFF = [500, 1000, 2000, 5000, 10000];

export class WebSocketClient {
  private readonly opts: WebSocketClientOptions;
  private readonly listeners: Listeners = {
    open: [],
    close: [],
    message: [],
    error: [],
  };
  private ws: WebSocketLike | null = null;
  private closed = false;
  private reconnectAttempt = 0;
  private reconnectTimer: unknown = null;

  constructor(opts: WebSocketClientOptions) {
    this.opts = opts;
  }

  on(event: "open", fn: Listener<void>): void;
  on(event: "close", fn: Listener<{ code: number; reason: string }>): void;
  on(event: "message", fn: Listener<WsEvent>): void;
  on(event: "error", fn: Listener<unknown>): void;
  on(event: WSEventName, fn: Listener<never>): void {
    switch (event) {
      case "open":
        this.listeners.open.push(fn as Listener<void>);
        return;
      case "close":
        this.listeners.close.push(
          fn as Listener<{ code: number; reason: string }>,
        );
        return;
      case "message":
        this.listeners.message.push(fn as Listener<WsEvent>);
        return;
      case "error":
        this.listeners.error.push(fn as Listener<unknown>);
        return;
    }
  }

  off(event: WSEventName, fn: Listener<never>): void {
    const arr = this.listeners[event] as Listener<unknown>[];
    const i = arr.indexOf(fn as Listener<unknown>);
    if (i >= 0) arr.splice(i, 1);
  }

  send(data: string): void {
    if (this.ws?.readyState !== 1) {
      throw new Error("websocket not open");
    }
    this.ws.send(data);
  }

  async connect(): Promise<void> {
    this.closed = false;
    await this.open();
  }

  close(code?: number, reason?: string): void {
    this.closed = true;
    if (this.reconnectTimer !== null) {
      const ct = this.opts.clearTimeout ?? clearTimeout;
      ct(this.reconnectTimer as Parameters<typeof clearTimeout>[0]);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close(code, reason);
      this.ws = null;
    }
  }

  private async open(): Promise<void> {
    const ticket: WSTicket = await this.opts.http.wsTicket();
    const url = buildWsUrl(
      this.opts.http.getBaseUrl(),
      ticket.ticket,
      this.opts.channelId,
    );
    const Ctor = this.opts.WebSocket ?? getGlobalWebSocket();
    const ws = new Ctor(url);
    this.ws = ws;
    ws.onopen = () => {
      this.reconnectAttempt = 0;
      for (const fn of this.listeners.open) fn();
    };
    ws.onmessage = (ev) => {
      const decoded = decodeFrame(ev.data);
      for (const fn of this.listeners.message) fn(decoded);
    };
    ws.onerror = (ev) => {
      for (const fn of this.listeners.error) fn(ev);
    };
    ws.onclose = (ev) => {
      this.ws = null;
      for (const fn of this.listeners.close) fn(ev);
      if (!this.closed && (this.opts.reconnect ?? true)) {
        this.scheduleReconnect();
      }
    };
  }

  private scheduleReconnect(): void {
    const schedule = this.opts.backoffMs ?? DEFAULT_BACKOFF;
    const idx = Math.min(this.reconnectAttempt, schedule.length - 1);
    const delay = schedule[idx] ?? schedule[schedule.length - 1] ?? 1000;
    this.reconnectAttempt += 1;
    const st = this.opts.setTimeout ?? setTimeout;
    this.reconnectTimer = st(() => {
      this.reconnectTimer = null;
      if (this.closed) return;
      void this.open().catch((err: unknown) => {
        for (const fn of this.listeners.error) fn(err);
        if (!this.closed) this.scheduleReconnect();
      });
    }, delay);
  }
}

export function buildWsUrl(
  base: string,
  ticket: string,
  channelId?: string,
): string {
  const u = new URL(base);
  if (u.protocol === "http:") u.protocol = "ws:";
  else if (u.protocol === "https:") u.protocol = "wss:";
  u.pathname = u.pathname.replace(/\/+$/, "") + "/ws";
  u.searchParams.set("ticket", ticket);
  if (channelId) u.searchParams.set("channel", channelId);
  return u.toString();
}

export function decodeFrame(raw: unknown): WsEvent {
  if (typeof raw !== "string") {
    return { type: "", data: raw };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { type: "", data: raw };
  }
  if (
    typeof parsed === "object" &&
    parsed !== null &&
    "type" in parsed &&
    typeof parsed.type === "string"
  ) {
    const data = "data" in parsed ? parsed.data : undefined;
    return { type: parsed.type, data };
  }
  return { type: "", data: parsed };
}

function getGlobalWebSocket(): WebSocketCtor {
  const g = globalThis as { WebSocket?: WebSocketCtor };
  if (!g.WebSocket) {
    throw new Error(
      "global WebSocket not available; pass opts.WebSocket explicitly",
    );
  }
  return g.WebSocket;
}

export async function* watch(
  http: Pick<HttpClient, "wsTicket" | "getBaseUrl">,
  channelId: string,
  opts: { WebSocket?: WebSocketCtor; signal?: AbortSignal } = {},
): AsyncGenerator<WsEvent, void, void> {
  const client = new WebSocketClient({
    http,
    channelId,
    WebSocket: opts.WebSocket,
    reconnect: false,
  });
  const queue: WsEvent[] = [];
  const state = { done: false };
  let resolveNext: ((ev: IteratorResult<WsEvent>) => void) | null = null;

  const push = (ev: WsEvent): void => {
    if (resolveNext) {
      const r = resolveNext;
      resolveNext = null;
      r({ value: ev, done: false });
    } else {
      queue.push(ev);
    }
  };
  const finish = (): void => {
    state.done = true;
    if (resolveNext) {
      const r = resolveNext;
      resolveNext = null;
      r({ value: undefined, done: true });
    }
  };

  client.on("message", push);
  client.on("close", finish);
  if (opts.signal) {
    opts.signal.addEventListener("abort", () => {
      client.close();
    });
  }
  await client.connect();
  try {
    for (;;) {
      if (state.done && queue.length === 0) return;
      const next = queue.shift();
      if (next !== undefined) {
        yield next;
        continue;
      }
      const ev = await new Promise<IteratorResult<WsEvent>>((resolve) => {
        resolveNext = resolve;
      });
      if (ev.done) return;
      yield ev.value;
    }
  } finally {
    client.close();
  }
}
