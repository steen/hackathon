import type * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

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

afterEach(() => {
  cleanup();
  meMock.mockReset();
  logoutMock.mockReset();
  writeTokenMock.mockReset();
  storedToken = null;
});

describe("test_web_auth_me_rehydrate_failure_surfaces_error", () => {
  it("clears the token but exposes a banner-ready error message when me() rejects", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockRejectedValue(new Error("network refused"));

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
    expect(banner).toHaveTextContent(/network refused/i);
    expect(writeTokenMock).toHaveBeenCalledWith(null);
  });
});

describe("test_web_auth_logout_server_failure_surfaces_error", () => {
  it("still clears local state but surfaces a server-not-acknowledged banner", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    logoutMock.mockRejectedValue(new Error("503 Service Unavailable"));

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
    expect(banner).toHaveTextContent(/503 service unavailable/i);
    expect(writeTokenMock).toHaveBeenCalledWith(null);
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
