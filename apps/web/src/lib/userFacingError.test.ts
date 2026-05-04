import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@hackathon/api-client";
import {
  REASON_CANCELED,
  REASON_GENERIC,
  REASON_INVALID_CREDENTIALS,
  REASON_NETWORK,
  REASON_SERVER_UNAVAILABLE,
  REASON_SESSION_INVALID,
  REASON_TIMEOUT,
  REASON_VALIDATION,
  bannerMessage,
  classifyError,
  classifyFormAuthError,
  formAuthMessage,
  userFacingMessage,
} from "./userFacingError.js";

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  consoleErrorSpy.mockRestore();
});

describe("classifyError", () => {
  it("maps ApiError 401/403 to session-invalid copy", () => {
    expect(classifyError(new ApiError(401, "unauthorized", "jwt sig mismatch"))).toBe(
      REASON_SESSION_INVALID,
    );
    expect(classifyError(new ApiError(403, "forbidden", "internal scope detail"))).toBe(
      REASON_SESSION_INVALID,
    );
  });

  it("maps ApiError 408 to timeout copy", () => {
    expect(classifyError(new ApiError(408, "timeout", "internal-trace-id"))).toBe(REASON_TIMEOUT);
  });

  it("maps ApiError 5xx to server-unavailable copy", () => {
    expect(classifyError(new ApiError(500, "internal", "db pool exhausted"))).toBe(
      REASON_SERVER_UNAVAILABLE,
    );
    expect(classifyError(new ApiError(503, "service_unavailable", "internal-detail-99"))).toBe(
      REASON_SERVER_UNAVAILABLE,
    );
  });

  it("falls back to generic for unmapped ApiError 4xx", () => {
    expect(classifyError(new ApiError(404, "not_found", "internal-detail"))).toBe(REASON_GENERIC);
    expect(classifyError(new ApiError(418, "teapot", "internal-detail"))).toBe(REASON_GENERIC);
  });

  it("maps AbortError to canceled copy", () => {
    const e = new Error("aborted internal-detail");
    e.name = "AbortError";
    expect(classifyError(e)).toBe(REASON_CANCELED);
  });

  it("maps TimeoutError to timeout copy", () => {
    const e = new Error("timed out internal-detail");
    e.name = "TimeoutError";
    expect(classifyError(e)).toBe(REASON_TIMEOUT);
  });

  it("maps fetch TypeError (offline/DNS/refused) to network copy", () => {
    expect(classifyError(new TypeError("Failed to fetch xyz-internal-detail"))).toBe(
      REASON_NETWORK,
    );
  });

  it("falls back to generic for plain Error", () => {
    expect(classifyError(new Error("boom internal-stack-trace"))).toBe(REASON_GENERIC);
  });

  it("falls back to generic for non-Error rejections (string, null, undefined, object)", () => {
    expect(classifyError("a-bare-string-rejection")).toBe(REASON_GENERIC);
    expect(classifyError(null)).toBe(REASON_GENERIC);
    expect(classifyError(undefined)).toBe(REASON_GENERIC);
    expect(classifyError({ message: "internal-detail-object" })).toBe(REASON_GENERIC);
  });

  it("never returns the raw err.message", () => {
    const raw = new Error("super-internal-secret-12345");
    const out = classifyError(raw);
    expect(out).not.toContain("super-internal-secret-12345");
  });
});

describe("classifyFormAuthError", () => {
  it("maps 401/403 to invalid-credentials (not session-invalid)", () => {
    expect(classifyFormAuthError(new ApiError(401, "unauthorized", "bad password"))).toBe(
      REASON_INVALID_CREDENTIALS,
    );
    expect(classifyFormAuthError(new ApiError(403, "forbidden", "internal-detail"))).toBe(
      REASON_INVALID_CREDENTIALS,
    );
  });

  it("maps 400/422 to validation copy", () => {
    expect(classifyFormAuthError(new ApiError(400, "bad_request", "username too short"))).toBe(
      REASON_VALIDATION,
    );
    expect(
      classifyFormAuthError(new ApiError(422, "unprocessable", "invite_code malformed internal")),
    ).toBe(REASON_VALIDATION);
  });

  it("falls through to classifyError for other shapes", () => {
    expect(classifyFormAuthError(new ApiError(503, "down", "internal"))).toBe(
      REASON_SERVER_UNAVAILABLE,
    );
    expect(classifyFormAuthError(new TypeError("Failed to fetch internal-detail"))).toBe(
      REASON_NETWORK,
    );
    expect(classifyFormAuthError(new Error("boom internal-trace"))).toBe(REASON_GENERIC);
  });

  it("never echoes raw err.message for any 4xx body", () => {
    const raw = new ApiError(401, "unauthorized", "secret-jwt-detail-99");
    const out = classifyFormAuthError(raw);
    expect(out).not.toContain("secret-jwt-detail-99");
    expect(out).not.toContain("401");
  });
});

describe("bannerMessage", () => {
  it("composes prefix + curated reason and taps console.error with the raw value", () => {
    const raw = new TypeError("Failed to fetch xyz-internal");
    const out = bannerMessage("Session could not be restored", raw);
    expect(out).toBe(`Session could not be restored: ${REASON_NETWORK}`);
    expect(consoleErrorSpy).toHaveBeenCalledWith("Session could not be restored", raw);
  });

  it("never includes the raw err.message in the returned string", () => {
    const raw = new Error("internal-stack-frame-secret");
    const out = bannerMessage("Operation failed", raw);
    expect(out).not.toContain("internal-stack-frame-secret");
  });
});

describe("userFacingMessage", () => {
  it("returns just the curated reason (no prefix) and still taps console.error", () => {
    const raw = new ApiError(503, "down", "db-internal");
    const out = userFacingMessage("Failed to load channels", raw);
    expect(out).toBe(REASON_SERVER_UNAVAILABLE);
    expect(consoleErrorSpy).toHaveBeenCalledWith("Failed to load channels", raw);
  });
});

describe("formAuthMessage", () => {
  it("composes a credentials reason for 401 form rejects", () => {
    const raw = new ApiError(401, "unauthorized", "wrong-password-internal");
    const out = formAuthMessage("Login failed", raw);
    expect(out).toBe(REASON_INVALID_CREDENTIALS);
    expect(out).not.toContain("wrong-password-internal");
    expect(consoleErrorSpy).toHaveBeenCalledWith("Login failed", raw);
  });

  it("composes a validation reason for 422 form rejects", () => {
    const raw = new ApiError(422, "unprocessable", "field internal-detail");
    expect(formAuthMessage("Registration failed", raw)).toBe(REASON_VALIDATION);
  });
});
