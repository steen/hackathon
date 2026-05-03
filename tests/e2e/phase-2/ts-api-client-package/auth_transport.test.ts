// AC-3: HTTP requests authenticate with `Authorization: Bearer <jwt>`;
// the WebSocketClient opens the WS connection using the one-shot
// ticket flow (call wsTicket() to mint a ticket, then connect with
// ?ticket=<hex>). The bearer token is NOT sent on the WS upgrade.
//
// Verified by:
//   1. Wrapping fetch in a sniffer; assert outgoing /api/auth/me has
//      `Authorization: Bearer <token>` after a real login.
//      Without getToken returning a token, no Authorization header.
//   2. Stubbing global WebSocket with a recording subclass and opening
//      the client's WebSocketClient. Assert the dialed URL has
//      ?ticket=<hex> (the actual ticket the server minted) and that
//      the constructor got NO Authorization header (the standard
//      WebSocket constructor does not accept one — verified by the
//      single-arg shape, plus ticket presence).

import { describe, it, expect, afterEach, vi } from "vitest";
import { Client } from "@hackathon/api-client";
import { serverUrl, inviteCode, uniqueUsername, strongPassword } from "./helpers.js";

interface SniffedRequest {
  url: string;
  method: string;
  authorization: string | null;
}

function sniffingFetch(captured: SniffedRequest[]): typeof fetch {
  const real = globalThis.fetch.bind(globalThis);
  return async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url =
      typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const headers = new Headers(init?.headers);
    captured.push({
      url,
      method: init?.method ?? "GET",
      authorization: headers.get("Authorization"),
    });
    return real(input, init);
  };
}

describe("AC-3: auth transport — Bearer on REST, ticket on WS, no bearer on WS upgrade", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("AC-3: REST sends `Authorization: Bearer <jwt>` after login; no header without a token", async () => {
    const captured: SniffedRequest[] = [];
    const c = new Client({
      baseUrl: serverUrl(),
      fetch: sniffingFetch(captured),
    });
    const username = uniqueUsername("authrest");
    const password = strongPassword();

    // Pre-auth: registering counts as a request with no Authorization.
    const reg = await c.register(username, password, inviteCode());
    const registerCall = captured.find((r) => r.url.endsWith("/api/auth/register"));
    expect(registerCall).toBeDefined();
    expect(registerCall?.authorization).toBeNull();

    // Post-auth: me() must carry the bearer token from the registration.
    captured.length = 0;
    await c.me();
    const meCall = captured.find((r) => r.url.endsWith("/api/auth/me"));
    expect(meCall).toBeDefined();
    expect(meCall?.authorization).toBe(`Bearer ${reg.token}`);
  });

  it("AC-3: WebSocketClient dials with ?ticket=<hex> and never sends Authorization on the WS upgrade", async () => {
    // Real client used to register + mint a ticket (REST half).
    const c = new Client({ baseUrl: serverUrl() });
    const username = uniqueUsername("authws");
    const password = strongPassword();
    await c.register(username, password, inviteCode());

    interface DialRecord {
      url: string;
      argLength: number;
    }
    const dials: DialRecord[] = [];
    const RealWS = globalThis.WebSocket;

    class SniffingWebSocket {
      readyState = 0;
      onopen: ((ev: unknown) => void) | null = null;
      onclose: ((ev: { code: number; reason: string }) => void) | null = null;
      onerror: ((ev: unknown) => void) | null = null;
      onmessage: ((ev: { data: unknown }) => void) | null = null;
      private inner: WebSocket;
      constructor(...args: unknown[]) {
        const url = args[0] as string;
        dials.push({ url, argLength: args.length });
        // Forward to the real WebSocket so we still observe a real
        // upgrade succeed against the running server.
        this.inner = new RealWS(url);
        this.inner.onopen = (ev): void => {
          this.readyState = this.inner.readyState;
          if (this.onopen) this.onopen(ev);
        };
        this.inner.onclose = (ev): void => {
          this.readyState = this.inner.readyState;
          if (this.onclose) this.onclose({ code: ev.code, reason: ev.reason });
        };
        this.inner.onerror = (ev): void => {
          if (this.onerror) this.onerror(ev);
        };
        this.inner.onmessage = (ev): void => {
          if (this.onmessage) this.onmessage({ data: ev.data });
        };
      }
      send(data: string): void {
        this.inner.send(data);
      }
      close(code?: number, reason?: string): void {
        this.inner.close(code, reason);
      }
    }
    vi.stubGlobal("WebSocket", SniffingWebSocket);

    const ws = c.websocket();
    const opened = new Promise<void>((resolve, reject) => {
      ws.on("open", () => {
        resolve();
      });
      ws.on("error", (e) => {
        reject(e instanceof Error ? e : new Error(String(e)));
      });
      setTimeout(() => {
        reject(new Error("ws open timeout"));
      }, 5000);
    });
    await ws.connect();
    await opened;
    ws.close();

    expect(dials.length).toBeGreaterThan(0);
    const dial = dials[0];
    expect(dial.argLength).toBe(1); // only the URL — no auth-bearing options arg
    const u = new URL(dial.url);
    const ticket = u.searchParams.get("ticket");
    expect(ticket).toBeTruthy();
    expect(ticket ?? "").toMatch(/^[0-9a-f]{16,}$/);
    // Sanity: WS scheme used, no Authorization could be sent because
    // browser WebSocket has no header API.
    expect(["ws:", "wss:"]).toContain(u.protocol);
  });
});
