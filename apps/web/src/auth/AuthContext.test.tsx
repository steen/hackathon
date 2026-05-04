import type * as React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@hackathon/api-client";

const meMock = vi.fn();
const logoutMock = vi.fn();
const writeTokenMock = vi.fn();

let storedToken: string | null = null;

vi.mock("../api.js", () => ({
  getClient: () => ({
    me: meMock,
    logout: logoutMock,
    login: vi.fn(),
    register: vi.fn(),
  }),
  readToken: (): string | null => storedToken,
  writeToken: (t: string | null): void => {
    storedToken = t;
    writeTokenMock(t);
  },
}));

import { AuthProvider, useAuth } from "./AuthContext.js";
import { ErrorBanner } from "../components/ErrorBanner.js";

function Probe(): React.JSX.Element {
  const { token, user, error, logout } = useAuth();
  return (
    <div>
      <span data-testid="token">{token ?? "none"}</span>
      <span data-testid="user">{user?.username ?? "none"}</span>
      <span data-testid="error">{error ?? "none"}</span>
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
  writeTokenMock.mockReset();
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
  it("hides the banner when the user clicks Dismiss", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockRejectedValue(new Error("boom"));

    render(
      <AuthProvider>
        <ErrorBanner />
        <Probe />
      </AuthProvider>,
    );

    const banner = await screen.findByTestId("auth-error-banner");
    expect(banner).toBeInTheDocument();

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
