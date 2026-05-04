// AC-4: Error-envelope decoding mirrors the server shape
// {ok, data, error: {code, message}}.
//
// Drives real server errors and asserts the rejected promise is an
// ApiError instance with .status and .code populated from the server
// envelope:
//   - bad-creds login -> 401
//   - unknown-channel post -> 404
//   - oversized REST body -> 413 + code "body_too_large"

import { describe, it, expect } from "vitest";
import { ApiError, createClient } from "@hackathon/api-client";
import { serverUrl, registerFresh } from "./helpers.js";

describe("AC-4: error envelope decoding mirrors the server {ok,data,error:{code,message}} shape", () => {
  it("AC-4: bad-credentials login rejects with ApiError(status=401)", async () => {
    const c = createClient({ baseUrl: serverUrl() });
    let caught: unknown = null;
    try {
      await c.login("nobody-" + Date.now().toString(), "wrong-password-1234567890");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ApiError);
    const e = caught as ApiError;
    expect(e.status).toBe(401);
    expect(typeof e.code).toBe("string");
    expect(e.code.length).toBeGreaterThan(0);
    expect(typeof e.message).toBe("string");
  });

  it("AC-4: posting to unknown channel rejects with ApiError(status=404)", async () => {
    const u = await registerFresh("envelope404");
    let caught: unknown = null;
    try {
      // Syntactically valid ULID (26 chars, Crockford base32) that is
      // guaranteed not to exist. The server's channel-id parser
      // returns 400 for malformed input and 404 for "valid id, no
      // such row" — we want the 404 path here to exercise the
      // not_found arm of the envelope decoder.
      await u.client.postMessage("00000000000000000000000000", "hi");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ApiError);
    const e = caught as ApiError;
    expect(e.status).toBe(404);
    expect(typeof e.code).toBe("string");
  });

  it("AC-4: oversized REST body rejects with ApiError(status=413, code=body_too_large)", async () => {
    const u = await registerFresh("envelope413");
    // Need a channel id to hit the messages endpoint; any valid
    // authed POST works because BodyCap fires before handler.
    const ch = await u.client.createChannel("c413-" + Math.random().toString(36).slice(2, 8));
    // 17 KiB > RESTBodyLimit (16 KiB).
    const huge = "x".repeat(17 * 1024);
    let caught: unknown = null;
    try {
      await u.client.postMessage(ch.id, huge);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(ApiError);
    const e = caught as ApiError;
    expect(e.status).toBe(413);
    expect(e.code).toBe("body_too_large");
  });
});
