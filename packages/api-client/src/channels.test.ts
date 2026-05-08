import { describe, expect, it } from "vitest";
import { HttpClient } from "./http.js";
import { markChannelRead } from "./channels.js";
import { FAKE_TOKEN, makeFetch } from "./test-helpers.js";

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
