import { describe, expect, it } from "vitest";
import { createClient } from "./client.js";
import type { FetchLike } from "./http.js";

const FAKE_TOKEN = "test-jwt-token-placeholder";

function urlOf(input: Parameters<FetchLike>[0]): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.toString();
  return input.url;
}

describe("Client", () => {
  it("login stores the token in memory and sends it on subsequent calls", async () => {
    const requests: { url: string; auth: string | undefined }[] = [];
    const fetchImpl: FetchLike = (input, init) => {
      const url = urlOf(input);
      const h = (init?.headers ?? {}) as Record<string, string>;
      requests.push({ url, auth: h.Authorization });
      if (url.endsWith("/api/auth/login")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              ok: true,
              data: {
                token: FAKE_TOKEN,
                user: { id: "U1", username: "alice" },
              },
              error: null,
            }),
            { status: 200 },
          ),
        );
      }
      return Promise.resolve(
        new Response(
          JSON.stringify({
            ok: true,
            data: { user: { id: "U1", username: "alice" } },
            error: null,
          }),
          { status: 200 },
        ),
      );
    };
    const c = createClient({ baseUrl: "http://srv", fetch: fetchImpl });
    await c.login("alice", "passw0rd-placeholder");
    await c.me();
    expect(requests[0]?.auth).toBeUndefined();
    expect(requests[1]?.auth).toBe(`Bearer ${FAKE_TOKEN}`);
  });

  it("logout clears the stored token", async () => {
    let storedToken: string | null = null;
    const seen: { url: string; auth: string | undefined }[] = [];
    const fetchImpl: FetchLike = (input, init) => {
      const url = urlOf(input);
      const h = (init?.headers ?? {}) as Record<string, string>;
      seen.push({ url, auth: h.Authorization });
      if (url.endsWith("/api/auth/login")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              ok: true,
              data: {
                token: FAKE_TOKEN,
                user: { id: "U1", username: "alice" },
              },
              error: null,
            }),
            { status: 200 },
          ),
        );
      }
      return Promise.resolve(
        new Response(JSON.stringify({ ok: true, data: { channels: [] }, error: null }), {
          status: 200,
        }),
      );
    };
    const c = createClient({
      baseUrl: "http://srv",
      fetch: fetchImpl,
      getToken: () => storedToken,
      setToken: (t) => {
        storedToken = t;
      },
    });
    await c.login("alice", "passw0rd-placeholder");
    expect(storedToken).toBe(FAKE_TOKEN);
    await c.logout();
    expect(storedToken).toBeNull();
    await c.listChannels();
    const last = seen[seen.length - 1];
    expect(last?.url).toBe("http://srv/api/channels");
    expect(last?.auth).toBeUndefined();
  });

  it("watch is exposed on the Client", () => {
    const fetchImpl: FetchLike = () => Promise.resolve(new Response("{}", { status: 200 }));
    const c = createClient({ baseUrl: "http://srv", fetch: fetchImpl });
    expect(typeof c.watch).toBe("function");
  });

  it("renameChannel hits PATCH /api/channels/{id} and returns the renamed channel", async () => {
    const calls: { method: string; url: string; body: string | null }[] = [];
    const fetchImpl: FetchLike = (input, init) => {
      calls.push({
        method: init?.method ?? "GET",
        url: urlOf(input),
        body: typeof init?.body === "string" ? init.body : null,
      });
      return Promise.resolve(
        new Response(
          JSON.stringify({
            ok: true,
            data: { id: "C1", name: "renamed", created_at: "2026-01-01T00:00:00Z" },
            error: null,
          }),
          { status: 200 },
        ),
      );
    };
    const c = createClient({ baseUrl: "http://srv", fetch: fetchImpl });
    const ch = await c.renameChannel("C1", "renamed");
    expect(ch.name).toBe("renamed");
    expect(calls[0]?.method).toBe("PATCH");
    expect(calls[0]?.url).toBe("http://srv/api/channels/C1");
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ name: "renamed" });
  });
});
