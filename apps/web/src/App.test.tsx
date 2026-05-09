import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";

const meMock = vi.fn();
const loginMock = vi.fn();
const registerMock = vi.fn();
const logoutMock = vi.fn();

let storedToken: string | null = null;

vi.mock("./api.js", () => ({
  getClient: () => ({
    me: meMock,
    login: loginMock,
    register: registerMock,
    logout: logoutMock,
  }),
  readToken: () => storedToken,
  writeToken: (v: string | null) => {
    storedToken = v;
  },
}));

vi.mock("./hooks/useChannels.js", () => ({
  useChannels: () => ({ channels: [], error: null, loading: false }),
}));

vi.mock("./hooks/usePresence.js", () => ({
  usePresence: () => ({ users: [], loading: false, error: null, lastEvent: null }),
}));

vi.mock("./hooks/useMessages.js", () => ({
  useMessages: () => ({
    messages: [],
    error: null,
    connection: "open",
    send: vi.fn(),
    retry: vi.fn(),
  }),
}));

vi.mock("./hooks/useDMs.js", () => ({
  useDMs: () => ({
    conversations: [],
    loading: false,
    error: null,
    reload: vi.fn(),
    startWith: vi.fn(),
  }),
}));

import { App } from "./App.js";
import { AuthProvider } from "./auth/AuthContext.js";

beforeEach(() => {
  storedToken = null;
  window.location.hash = "";
});

afterEach(() => {
  cleanup();
  meMock.mockReset();
  loginMock.mockReset();
  registerMock.mockReset();
  logoutMock.mockReset();
});

function renderApp(): void {
  render(
    <AuthProvider>
      <App />
    </AuthProvider>,
  );
}

describe("test_web_app_focus_management_login_mount", () => {
  it("focuses the username input on the login screen at mount", async () => {
    renderApp();
    const username = await screen.findByLabelText(/username/i);
    await waitFor(() => {
      expect(document.activeElement).toBe(username);
    });
  });
});

describe("test_web_app_focus_management_logout_returns_to_login_username", () => {
  it("re-mounts <Login /> on logout and focuses the username input", async () => {
    storedToken = "test-jwt-token-placeholder";
    meMock.mockResolvedValue({ id: "U1", username: "alice" });
    logoutMock.mockResolvedValue(undefined);
    renderApp();

    const signOut = await screen.findByRole("button", { name: /sign out/i });
    act(() => {
      signOut.click();
    });

    const username = await screen.findByLabelText(/username/i);
    await waitFor(() => {
      expect(document.activeElement).toBe(username);
    });
  });
});
