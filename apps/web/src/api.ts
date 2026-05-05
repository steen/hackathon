import { createClient, type Client } from "@hackathon/api-client";

const TOKEN_KEY = "hackathon.token";

function readToken(): string | null {
  try {
    return globalThis.localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

function writeToken(token: string | null): void {
  try {
    if (token === null) globalThis.localStorage.removeItem(TOKEN_KEY);
    else globalThis.localStorage.setItem(TOKEN_KEY, token);
  } catch {
    /* ignore quota / disabled storage */
  }
}

function defaultBaseUrl(): string {
  const env = (import.meta as { env?: Record<string, string | undefined> }).env;
  const fromEnv = env?.VITE_API_BASE_URL;
  if (fromEnv && fromEnv.length > 0) return fromEnv;
  if (typeof window !== "undefined" && window.location.origin) {
    return window.location.origin;
  }
  return "http://127.0.0.1:8080";
}

let cached: Client | null = null;

export function getClient(): Client {
  cached ??= createClient({
    baseUrl: defaultBaseUrl(),
    getToken: readToken,
    setToken: writeToken,
  });
  return cached;
}

// Cleanup contract: this helper only swaps the cached pointer. It does not
// call any cleanup hook on the outgoing Client. Tests that install a custom
// Client with mutable state (custom setToken sink, custom wsCtor, etc.) own
// resetting that state between cases — passing `null` here will not flush it.
// The default path (getClient() → createClient(...)) is unaffected because a
// fresh Client is constructed on the next getClient() call.
export function setClientForTesting(c: Client | null): void {
  cached = c;
}

export { TOKEN_KEY, readToken, writeToken };
