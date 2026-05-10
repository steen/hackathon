import type * as React from "react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@hackathon/api-client";

const meMock = vi.fn();
const logoutMock = vi.fn();
const loginMock = vi.fn();
const registerMock = vi.fn();
const writeTokenMock = vi.fn();
const writeIdentitySeedMock = vi.fn();
const clearIdentitySeedMock = vi.fn();

let storedToken: string | null = null;

vi.mock("../api.js", () => ({
  getClient: () => ({
    me: meMock,
    logout: logoutMock,
    login: loginMock,
    register: registerMock,
  }),
  readToken: (): string | null => storedToken,
  writeToken: (t: string | null): void => {
    storedToken = t;
    writeTokenMock(t);
  },
}));

vi.mock("../lib/identityStore.js", () => ({
  writeIdentitySeed: async (userId: string, seed: Uint8Array): Promise<void> => {
    await writeIdentitySeedMock(userId, seed);
  },
  clearIdentitySeed: async (): Promise<void> => {
    await clearIdentitySeedMock();
  },
}));

// Stub the identity-derivation surface so the test does not pay an
// Argon2id round (~250ms) and does not require libsodium-wrappers-sumo
// to be initialized under jsdom. The canary's behavior depends only on
// the b64-encoded sign_pubkey comparison, so a fixed local pubkey
// stand-in is enough to drive both the "skip" and the eventual
// (already-covered-by-Login.tsx unit tests in a sibling file) "throw"
// branches.
vi.mock("@hackathon/api-client", async () => {
  const actual = await vi.importActual<Record<string, unknown>>("@hackathon/api-client");
  const stubBoxPub = new Uint8Array(32).fill(1);
  const stubSignPub = new Uint8Array(32).fill(2);
  const stubBoxPriv = new Uint8Array(32).fill(3);
  const stubSignPriv = new Uint8Array(64).fill(4);
  const stubRootSeed = new Uint8Array(32).fill(5);
  return {
    ...actual,
    ready: (): Promise<void> => Promise.resolve(),
    deriveIdentity: (): Promise<{
      rootSeed: Uint8Array;
      boxSeed: Uint8Array;
      signSeed: Uint8Array;
      boxPub: Uint8Array;
      boxPriv: Uint8Array;
      signPub: Uint8Array;
      signPriv: Uint8Array;
    }> =>
      Promise.resolve({
        rootSeed: stubRootSeed,
        boxSeed: new Uint8Array(32),
        signSeed: new Uint8Array(32),
        boxPub: stubBoxPub,
        boxPriv: stubBoxPriv,
        signPub: stubSignPub,
        signPriv: stubSignPriv,
      }),
    b64: (raw: Uint8Array): string => `b64:${String(raw[0] ?? 0)}:${String(raw.length)}`,
  };
});

import { AuthProvider, useAuth } from "./AuthContext.js";
import { ErrorBanner } from "../components/ErrorBanner.js";

function Probe(): React.JSX.Element {
  const { token, user, error, login, register, logout } = useAuth();
  return (
    <div>
      <span data-testid="token">{token ?? "none"}</span>
      <span data-testid="user">{user?.username ?? "none"}</span>
      <span data-testid="error">{error ?? "none"}</span>
      <button
        type="button"
        onClick={() => {
          void login("alice", "pw");
        }}
      >
        sign-in
      </button>
      <button
        type="button"
        onClick={() => {
          void register("alice", "pw", "invite-code-placeholder");
        }}
      >
        sign-up
      </button>
      <button
        type="button"
        onClick={() => {
          void logout();
        }}
      >
        sign-out
      </button>
    </div>
  );
}

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  meMock.mockReset();
  logoutMock.mockReset();
  loginMock.mockReset();
  registerMock.mockReset();
  writeTokenMock.mockReset();
  writeIdentitySeedMock.mockReset();
  clearIdentitySeedMock.mockReset();
  storedToken = null;
  consoleErrorSpy.mockRestore();
});

describe("test_web_auth_me_rehydrate_failure_surfaces_error", () => {
  it("clears the token and shows curated network copy without echoing the raw error", async () => {
    storedToken = "test-jwt-token-placeholder";
    const raw = new TypeError("Failed to fetch xyz-internal-detail");
    meMock.mockRejectedValue(raw);

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("token")).toHaveTextContent("none");
    });
    expect(screen.getByTestId("user")).toHaveTextContent("none");
    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/session could not be restored/i);
    expect(banner).toHaveTextContent(/could not reach the server/i);
    expect(banner).not.toHaveTextContent(/xyz-internal-detail/i);
    expect(banner).not.toHaveTextContent(/failed to fetch/i);
    expect(writeTokenMock).toHaveBeenCalledWith(null);
    expect(consoleErrorSpy).toHaveBeenCalledWith("Session could not be restored", raw);
  });

  it("maps an ApiError 5xx to the server-unavailable copy", async () => {
    storedToken = "test-jwt-token-placeholder";
    const raw = new ApiError(503, "service_unavailable", "internal db pool exhausted");
    meMock.mockRejectedValue(raw);

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/server is having trouble/i);
    expect(banner).not.toHaveTextContent(/internal db pool exhausted/i);
    expect(banner).not.toHaveTextContent(/503/);
  });

  it("maps an ApiError 401 to the session-invalid copy", async () => {
    storedToken = "test-jwt-token-placeholder";
    const raw = new ApiError(401, "unauthorized", "jwt sig mismatch");
    meMock.mockRejectedValue(raw);

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/session is no longer valid/i);
    expect(banner).not.toHaveTextContent(/jwt sig mismatch/i);
  });

  it("falls back to a generic message for unrecognized error shapes", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockRejectedValue("a-bare-string-rejection-with-details");

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/something went wrong/i);
    expect(banner).not.toHaveTextContent(/a-bare-string-rejection-with-details/i);
  });

  it("maps an AbortError to the canceled copy", async () => {
    storedToken = "test-jwt-token-placeholder";
    const raw = Object.assign(new Error("aborted-internal-detail"), { name: "AbortError" });
    meMock.mockRejectedValue(raw);

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/request was canceled/i);
    expect(banner).not.toHaveTextContent(/aborted-internal-detail/i);
  });
});

describe("test_web_auth_logout_server_failure_surfaces_error", () => {
  it("clears local state and shows curated copy without leaking raw err.message", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    const raw = new ApiError(
      503,
      "service_unavailable",
      "Service Unavailable internal-trace-id-99",
    );
    logoutMock.mockRejectedValue(raw);

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("user")).toHaveTextContent("alice");
    });

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /sign-out/i }));

    await waitFor(() => {
      expect(screen.getByTestId("token")).toHaveTextContent("none");
    });
    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/server did not acknowledge/i);
    expect(banner).toHaveTextContent(/server is having trouble/i);
    expect(banner).not.toHaveTextContent(/internal-trace-id-99/i);
    expect(banner).not.toHaveTextContent(/503/);
    expect(writeTokenMock).toHaveBeenCalledWith(null);
    expect(consoleErrorSpy).toHaveBeenCalledWith(
      "Signed out locally, but the server did not acknowledge",
      raw,
    );
  });
});

describe("test_web_auth_error_banner_dismiss_clears_error", () => {
  it("hides the curated network banner when the user clicks Dismiss", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockRejectedValue(new TypeError("Failed to fetch"));

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/could not reach the server/i);

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /dismiss error/i }));

    await waitFor(() => {
      expect(screen.queryByTestId("auth-error-banner")).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("error")).toHaveTextContent("none");
  });
});

describe("test_web_auth_successful_login_path_has_no_error", () => {
  it("renders no banner when me() resolves", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockResolvedValue({ id: "U1", username: "alice" });

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("user")).toHaveTextContent("alice");
    });
    expect(screen.queryByTestId("auth-error-banner")).not.toBeInTheDocument();
  });
});

describe("test_web_auth_successful_login_clears_prior_error", () => {
  it("removes a stale rehydrate error after login() resolves", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockRejectedValue(new TypeError("Failed to fetch"));
    loginMock.mockResolvedValue({
      token: "fresh-jwt-token-placeholder",
      user: { id: "U1", username: "alice" },
    });

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/session could not be restored/i);

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /sign-in/i }));

    await waitFor(() => {
      expect(screen.queryByTestId("auth-error-banner")).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("error")).toHaveTextContent("none");
    expect(screen.getByTestId("token")).toHaveTextContent("fresh-jwt-token-placeholder");
    expect(screen.getByTestId("user")).toHaveTextContent("alice");
    expect(loginMock).toHaveBeenCalledWith("alice", "pw");
    expect(writeTokenMock).toHaveBeenLastCalledWith("fresh-jwt-token-placeholder");
  });
});

describe("test_web_auth_successful_register_clears_prior_error", () => {
  it("removes a stale rehydrate error after register() resolves", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockRejectedValue(new TypeError("Failed to fetch"));
    registerMock.mockResolvedValue({
      token: "fresh-jwt-token-placeholder",
      user: { id: "U2", username: "bob" },
    });

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toHaveTextContent(/session could not be restored/i);

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /sign-up/i }));

    await waitFor(() => {
      expect(screen.queryByTestId("auth-error-banner")).not.toBeInTheDocument();
    });
    expect(screen.getByTestId("error")).toHaveTextContent("none");
    expect(screen.getByTestId("token")).toHaveTextContent("fresh-jwt-token-placeholder");
    expect(screen.getByTestId("user")).toHaveTextContent("bob");
    expect(registerMock).toHaveBeenCalledWith("alice", "pw", "invite-code-placeholder", undefined);
    expect(writeTokenMock).toHaveBeenLastCalledWith("fresh-jwt-token-placeholder");
  });
});

// PassphraseProbe drives the canary path: it calls `login()` with a
// non-empty `identityPassphrase` so the AuthContext.tsx:90-105 block
// runs (skip when remote sign_pubkey is empty/absent; throw
// WrongPassphraseError when it disagrees with the locally-derived one).
function PassphraseProbe({ passphrase }: { passphrase: string }): React.JSX.Element {
  const { token, user, error, login } = useAuth();
  const [thrown, setThrown] = useState<string | null>(null);
  return (
    <div>
      <span data-testid="token">{token ?? "none"}</span>
      <span data-testid="user">{user?.username ?? "none"}</span>
      <span data-testid="error">{error ?? "none"}</span>
      <span data-testid="thrown">{thrown ?? "none"}</span>
      <button
        type="button"
        onClick={() => {
          void (async (): Promise<void> => {
            try {
              await login("alice", "pw", passphrase);
            } catch (err) {
              setThrown(err instanceof Error ? err.name : "non-error-throw");
            }
          })();
        }}
      >
        sign-in-with-passphrase
      </button>
    </div>
  );
}

describe("test_web_auth_canary_skips_when_remote_sign_pubkey_is_empty", () => {
  it("does not throw WrongPassphraseError and still persists the seed when sign_pubkey is absent", async () => {
    // Legacy / pre-Phase-10 user: server returns the JWT but the
    // user row has no identity pubkeys yet. The canary must skip so
    // the user can still sign in; writeIdentitySeed must still run so
    // the next /me wave can register-the-keys.
    loginMock.mockResolvedValue({
      token: "fresh-jwt-token-placeholder",
      user: { id: "U-legacy", username: "alice" },
      // sign_pubkey absent on purpose.
    });
    writeIdentitySeedMock.mockResolvedValue(undefined);

    render(
      <AuthProvider>
        <PassphraseProbe passphrase="passphrase-twelve-or-more" />
      </AuthProvider>,
    );

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /sign-in-with-passphrase/i }));

    await waitFor(() => {
      expect(screen.getByTestId("token")).toHaveTextContent("fresh-jwt-token-placeholder");
    });
    expect(screen.getByTestId("user")).toHaveTextContent("alice");
    expect(screen.getByTestId("thrown")).toHaveTextContent("none");
    expect(writeIdentitySeedMock).toHaveBeenCalledTimes(1);
    expect(writeIdentitySeedMock).toHaveBeenCalledWith("U-legacy", expect.any(Uint8Array));
    // Server-side logout must NOT have been triggered — that path is
    // exclusive to the wrong-passphrase branch.
    expect(logoutMock).not.toHaveBeenCalled();
    expect(writeTokenMock).toHaveBeenLastCalledWith("fresh-jwt-token-placeholder");
  });

  it("also skips when sign_pubkey is explicitly the empty string", async () => {
    loginMock.mockResolvedValue({
      token: "fresh-jwt-token-placeholder",
      user: { id: "U-legacy", username: "alice", sign_pubkey: "" },
    });
    writeIdentitySeedMock.mockResolvedValue(undefined);

    render(
      <AuthProvider>
        <PassphraseProbe passphrase="passphrase-twelve-or-more" />
      </AuthProvider>,
    );

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /sign-in-with-passphrase/i }));

    await waitFor(() => {
      expect(screen.getByTestId("token")).toHaveTextContent("fresh-jwt-token-placeholder");
    });
    expect(screen.getByTestId("thrown")).toHaveTextContent("none");
    expect(writeIdentitySeedMock).toHaveBeenCalledTimes(1);
    expect(logoutMock).not.toHaveBeenCalled();
  });
});
