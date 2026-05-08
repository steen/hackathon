import { describe, expect, it } from "vitest";
import { HttpClient } from "./http.js";
import { ApiError, isApiErrorCode } from "./errors.js";
import { FAKE_INVITE, FAKE_TOKEN, makeFetch } from "./test-helpers.js";

describe("HttpClient", () => {
  it("login decodes the envelope and returns token+user", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: {
            token: FAKE_TOKEN,
            user: { id: "U1", username: "alice" },
          },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    const out = await c.login("alice", "passw0rd-placeholder");
    expect(out.token).toBe(FAKE_TOKEN);
    expect(out.user.username).toBe("alice");
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://srv/api/auth/login");
    expect(calls[0]?.headers["Content-Type"]).toBe("application/json");
    expect(calls[0]?.body).toBe(
      JSON.stringify({ username: "alice", password: "passw0rd-placeholder" }),
    );
  });

  it("register posts invite_code in the wire body", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 201,
        body: {
          ok: true,
          data: { token: FAKE_TOKEN, user: { id: "U1", username: "bob" } },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await c.register("bob", "passw0rd-placeholder", FAKE_INVITE);
    expect(calls[0]?.url).toBe("http://srv/api/auth/register");
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({
      username: "bob",
      password: "passw0rd-placeholder",
      invite_code: FAKE_INVITE,
    });
  });

  it("me returns the unwrapped user record", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: { user: { id: "U1", username: "alice" } },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    const u = await c.me();
    expect(u.id).toBe("U1");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
    expect(calls[0]?.url).toBe("http://srv/api/auth/me");
  });

  it("logout posts to /api/auth/logout", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { ok: true }, error: null } },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    await c.logout();
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://srv/api/auth/logout");
  });

  it("wsTicket hits /api/auth/ws-ticket and returns the ticket", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: { ticket: "deadbeef", expires_at: "2026-01-01T00:00:00Z" },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    const t = await c.wsTicket();
    expect(t.ticket).toBe("deadbeef");
    expect(calls[0]?.url).toBe("http://srv/api/auth/ws-ticket");
    expect(calls[0]?.method).toBe("POST");
  });

  it("listChannels unwraps {channels:[...]} and includes bearer", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: {
            channels: [{ id: "C1", name: "general", created_at: "2026-01-01T00:00:00Z" }],
          },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    const out = await c.listChannels();
    expect(out).toHaveLength(1);
    expect(out[0]?.name).toBe("general");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
  });

  it("createChannel posts the name body", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 201,
        body: {
          ok: true,
          data: { id: "C1", name: "lobby", created_at: "2026-01-01T00:00:00Z" },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    const ch = await c.createChannel("lobby");
    expect(ch.id).toBe("C1");
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ name: "lobby" });
  });

  it("renameChannel PATCHes /api/channels/{id} with {name} and unwraps the channel", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: { id: "C1", name: "renamed", created_at: "2026-01-01T00:00:00Z" },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    const ch = await c.renameChannel("C1", "renamed");
    expect(ch.name).toBe("renamed");
    expect(calls[0]?.method).toBe("PATCH");
    expect(calls[0]?.url).toBe("http://srv/api/channels/C1");
    expect(calls[0]?.headers["Content-Type"]).toBe("application/json");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ name: "renamed" });
  });

  it("renameChannel percent-encodes the channel id in the path", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: { id: "C/1", name: "ok", created_at: "2026-01-01T00:00:00Z" },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await c.renameChannel("C/1", "ok");
    expect(calls[0]?.url).toBe("http://srv/api/channels/C%2F1");
  });

  it.each([
    [400, "invalid_name", "channel name must be 1..64 chars"],
    [403, "forbidden", "not the channel owner"],
    [404, "not_found", "no such channel"],
    [409, "conflict", "channel name already taken"],
    [429, "rate_limited", "too many rename requests"],
    [500, "internal", "internal error"],
  ])("renameChannel maps %i → typed ApiError(%s)", async (status, code, message) => {
    const { fetch } = makeFetch([
      {
        status,
        body: { ok: false, data: null, error: { code, message } },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await expect(c.renameChannel("C1", "anything")).rejects.toMatchObject({
      status,
      code,
      message,
    });
  });

  it("listMessages forwards before+limit and unwraps {messages:[]}", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { messages: [] }, error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await c.listMessages("CHAN1", { before: "M99", limit: 25 });
    expect(calls[0]?.url).toBe("http://srv/api/channels/CHAN1/messages?before=M99&limit=25");
  });

  it("postMessage posts {body} and returns the message", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 201,
        body: {
          ok: true,
          data: {
            id: "M1",
            channel_id: "CHAN1",
            sender_user_id: "U1",
            body: "hi",
            created_at: "2026-01-01T00:00:00Z",
          },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    const m = await c.postMessage("CHAN1", "hi");
    expect(m.id).toBe("M1");
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ body: "hi" });
    expect(calls[0]?.url).toBe("http://srv/api/channels/CHAN1/messages");
  });

  it("decodes error envelope into ApiError carrying code+status", async () => {
    const { fetch } = makeFetch([
      {
        status: 401,
        body: {
          ok: false,
          data: null,
          error: { code: "unauthorized", message: "invalid credentials" },
        },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await expect(c.login("alice", "wrong-password-placeholder")).rejects.toMatchObject({
      code: "unauthorized",
      status: 401,
      message: "invalid credentials",
    });
  });

  it("isApiErrorCode discriminates by code", () => {
    const err = new ApiError(409, "conflict", "channel name already taken");
    expect(isApiErrorCode(err, "conflict")).toBe(true);
    expect(isApiErrorCode(err, "not_found")).toBe(false);
    expect(isApiErrorCode(new Error("x"), "conflict")).toBe(false);
  });
});
