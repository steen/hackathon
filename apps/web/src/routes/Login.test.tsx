import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@hackathon/api-client";

const loginMock = vi.fn();
const meMock = vi.fn();
const logoutMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    login: loginMock,
    me: meMock,
    logout: logoutMock,
  }),
  readToken: () => null,
  writeToken: vi.fn(),
}));

import { AuthProvider } from "../auth/AuthContext.js";
import { Login } from "./Login.js";

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  loginMock.mockReset();
  meMock.mockReset();
  logoutMock.mockReset();
  consoleErrorSpy.mockRestore();
});

describe("test_web_login_focuses_username_on_mount", () => {
  it("places focus on the username input when <Login /> mounts", async () => {
    render(
      <AuthProvider>
        <Login onSwitchToRegister={() => undefined} />
      </AuthProvider>,
    );
    const u = screen.getByLabelText(/username/i);
    await waitFor(() => {
      expect(document.activeElement).toBe(u);
    });
  });
});

describe("test_web_login_form_renders_curated_error_without_leaking_raw_message", () => {
  it("shows invalid-credentials copy on 401 without echoing the server detail", async () => {
    const raw = new ApiError(401, "unauthorized", "internal-jwt-detail-leak-99");
    loginMock.mockRejectedValue(raw);
    render(
      <AuthProvider>
        <Login onSwitchToRegister={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "alice");
    await u.type(screen.getByLabelText(/password/i), "wrong-passw0rd-placeholder");
    await u.click(screen.getByRole("button", { name: /sign in/i }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/that username and password don't match/i);
    expect(alert).not.toHaveTextContent(/internal-jwt-detail-leak-99/i);
    expect(alert).not.toHaveTextContent(/401/);
    expect(consoleErrorSpy).toHaveBeenCalledWith("Login failed", raw);
  });

  it("shows server-unavailable copy on 503 without echoing the server detail", async () => {
    const raw = new ApiError(503, "service_unavailable", "db-pool-internal-trace-77");
    loginMock.mockRejectedValue(raw);
    render(
      <AuthProvider>
        <Login onSwitchToRegister={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "alice");
    await u.type(screen.getByLabelText(/password/i), "passw0rd-placeholder");
    await u.click(screen.getByRole("button", { name: /sign in/i }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/server is having trouble/i);
    expect(alert).not.toHaveTextContent(/db-pool-internal-trace-77/i);
    expect(alert).not.toHaveTextContent(/503/);
  });

  it("shows network copy on a fetch TypeError without echoing the raw message", async () => {
    const raw = new TypeError("Failed to fetch xyz-internal-detail");
    loginMock.mockRejectedValue(raw);
    render(
      <AuthProvider>
        <Login onSwitchToRegister={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "alice");
    await u.type(screen.getByLabelText(/password/i), "passw0rd-placeholder");
    await u.click(screen.getByRole("button", { name: /sign in/i }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/could not reach the server/i);
    expect(alert).not.toHaveTextContent(/xyz-internal-detail/i);
    expect(alert).not.toHaveTextContent(/failed to fetch/i);
  });
});

describe("test_web_login_form_calls_login_endpoint", () => {
  it("submits username/password to client.login", async () => {
    loginMock.mockResolvedValue({
      token: "test-jwt-token-placeholder",
      user: { id: "U1", username: "alice" },
    });
    render(
      <AuthProvider>
        <Login onSwitchToRegister={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "alice");
    await u.type(screen.getByLabelText(/password/i), "passw0rd-placeholder");
    await u.click(screen.getByRole("button", { name: /sign in/i }));
    expect(loginMock).toHaveBeenCalledTimes(1);
    expect(loginMock).toHaveBeenCalledWith("alice", "passw0rd-placeholder");
  });
});
