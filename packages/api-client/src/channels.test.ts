import { describe, expect, it } from "vitest";
import { HttpClient, type FetchLike } from "./http.js";
import { markChannelRead } from "./channels.js";

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

describe("channels wrappers", () => {
  it("markChannelRead POSTs {message_id} to the channel read path", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { ok: true }, error: null } },
    ]);
    const c = new HttpClient({
      baseUrl: "http://srv",
      fetch,
      getToken: () => FAKE_TOKEN,
    });
    await markChannelRead(c, "CHAN1", "M42");
    expect(calls[0]?.method).toBe("POST");
    expect(calls[0]?.url).toBe("http://srv/api/channels/CHAN1/read");
    expect(calls[0]?.headers["Content-Type"]).toBe("application/json");
    expect(calls[0]?.headers.Authorization).toBe(`Bearer ${FAKE_TOKEN}`);
    expect(JSON.parse(calls[0]?.body ?? "{}")).toEqual({ message_id: "M42" });
  });

  it("markChannelRead percent-encodes the channel id in the path", async () => {
    const { fetch, calls } = makeFetch([
      { status: 200, body: { ok: true, data: { ok: true }, error: null } },
    ]);
    const c = new HttpClient({ baseUrl: "http://srv", fetch });
    await markChannelRead(c, "C/1", "M42");
    expect(calls[0]?.url).toBe("http://srv/api/channels/C%2F1/read");
  });
});
