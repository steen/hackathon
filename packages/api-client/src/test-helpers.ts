import type { FetchLike } from "./http.js";

export interface RecordedCall {
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string | null;
}

export interface FakeResponse {
  status: number;
  body: unknown;
}

export function makeFetch(responses: FakeResponse[]): {
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

export const FAKE_TOKEN = "test-jwt-token-placeholder";
export const FAKE_INVITE = "test-invite-code-placeholder";
