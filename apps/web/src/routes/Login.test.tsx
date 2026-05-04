import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

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

afterEach(() => {
  cleanup();
  loginMock.mockReset();
  meMock.mockReset();
  logoutMock.mockReset();
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
