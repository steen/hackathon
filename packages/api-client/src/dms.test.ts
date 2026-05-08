import { describe, expect, it } from "vitest";
import { HttpClient, type FetchLike } from "./http.js";
import { createDM, listDMMessages, listDMs, markDMRead, sendDMMessage } from "./dms.js";

interface RecordedCall {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string | null;
}

interface FakeResponse {
  status: number;
  body: unknown;
}

function makeFetch(responses: FakeResponse[]): {
  fetch: FetchLike;
  calls: RecordedCall[];
} {
  const calls: RecordedCall[] = [];
  let i = 0;
  const fetchImpl: FetchLike = (input, init) => {
    let url: string;
    if (typeof input === "string") url = input;
    else if (input instanceof URL) url = input.toString();
    else url = input.url;
    const headers: Record<string, string> = {};
    const h = init?.headers;
    if (h && typeof h === "object" && !Array.isArray(h)) {
      for (const [k, v] of Object.entries(h as Record<string, string>)) {
        headers[k] = v;
      }
    }
    calls.push({
      method: init?.method ?? "GET",
      url,
      headers,
      body: typeof init?.body === "string" ? init.body : null,
    });
    const r = responses[i] ?? responses[responses.length - 1];
    i += 1;
    if (!r) throw new Error("no fake response configured");
    return Promise.resolve(
      new Response(JSON.stringify(r.body), {
        status: r.status,
        headers: { "Content-Type": "application/json" },
      }),
    );
  };
  return { fetch: fetchImpl, calls };
}

const FAKE_TOKEN = "test-jwt-token-placeholder";

function conversationFixture(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "DM1",
    user_a_id: "U1",
    user_b_id: "U2",
    last_message_id: null,
    last_message_at: null,
    created_at: "2026-01-01T00:00:00Z",
    peer: { id: "U2", username: "bob" },
    unread_count: 0,
    ...overrides,
  };
}

function dmMessageFixture(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "DMSG1",
    conversation_id: "DM1",
    sender_user_id: "U1",
    body: "hi",
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("dms wrappers", () => {
  it("createDM POSTs /api/dms with {peer_user_id} and unwraps the conversation", async () => {
    const { fetch, calls } = makeFetch([
      { status: 201, body: { ok: true, data: conversationFixture(), error: null } },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    const conv = await createDM(c, "U2");
    expect(conv.id).toBe("DM1");
    expect(conv.peer.username).toBe("bob");
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://srv/api/dms");
    expect(calls[0]?.headers["Content-Type"]).toBe("application/json");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ peer_user_id: "U2" });
  });

  it("createDM accepts the 200 idempotent response shape too", async () => {
    const { fetch } = makeFetch([
      { status: 200, body: { ok: true, data: conversationFixture(), error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    const conv = await createDM(c, "U2");
    expect(conv.id).toBe("DM1");
  });

  it("listDMs unwraps {conversations:[...]} and includes bearer", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: {
          ok: true,
          data: { conversations: [conversationFixture(), conversationFixture({ id: "DM2" })] },
          error: null,
        },
      },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    const out = await listDMs(c);
    expect(out).toHaveLength(2);
    expect(out[0]?.id).toBe("DM1");
    expect(out[1]?.id).toBe("DM2");
    expect(calls[0]?.method).toBe("GET");
    expect(calls[0]?.url).toBe("http://srv/api/dms");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
  });

  it("sendDMMessage POSTs {body} to the conversation messages path", async () => {
    const { fetch, calls } = makeFetch([
      { status: 201, body: { ok: true, data: dmMessageFixture(), error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    const m = await sendDMMessage(c, "DM1", "hi");
    expect(m.id).toBe("DMSG1");
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM1/messages");
    expect(calls[0]?.headers["Content-Type"]).toBe("application/json");
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ body: "hi" });
  });

  it("sendDMMessage percent-encodes the conversation id in the path", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 201,
        body: { ok: true, data: dmMessageFixture({ conversation_id: "DM/1" }), error: null },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await sendDMMessage(c, "DM/1", "hi");
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM%2F1/messages");
  });

  it("listDMMessages forwards before+limit and unwraps {messages:[...]}", async () => {
    const { fetch, calls } = makeFetch([
      {
        status: 200,
        body: { ok: true, data: { messages: [dmMessageFixture()] }, error: null },
      },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    const out = await listDMMessages(c, "DM1", { before: "DMSG99", limit: 25 });
    expect(out).toHaveLength(1);
    expect(out[0]?.id).toBe("DMSG1");
    expect(calls[0]?.method).toBe("GET");
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM1/messages?before=DMSG99&limit=25");
  });

  it("listDMMessages omits the query string when no opts are supplied", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { messages: [] }, error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await listDMMessages(c, "DM1");
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM1/messages");
  });

  it("listDMMessages percent-encodes the conversation id in the path", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { messages: [] }, error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await listDMMessages(c, "DM/1", { limit: 10 });
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM%2F1/messages?limit=10");
  });

  it("markDMRead POSTs {message_id} to the conversation read path", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { ok: true }, error: null } },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    await markDMRead(c, "DM1", "DMSG42");
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM1/read");
    expect(calls[0]?.headers["Content-Type"]).toBe("application/json");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ message_id: "DMSG42" });
  });

  it("markDMRead percent-encodes the conversation id in the path", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { ok: true }, error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await markDMRead(c, "DM/1", "DMSG42");
    expect(calls[0]?.url).toBe("http://srv/api/dms/DM%2F1/read");
  });
});
