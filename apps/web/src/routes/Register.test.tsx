import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@hackathon/api-client";

const registerMock = vi.fn();
const meMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    register: registerMock,
    me: meMock,
    logout: vi.fn(),
  }),
  readToken: () => null,
  writeToken: vi.fn(),
}));

import { AuthProvider } from "../auth/AuthContext.js";
import { Register } from "./Register.js";

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  registerMock.mockReset();
  meMock.mockReset();
  consoleErrorSpy.mockRestore();
});

describe("test_web_register_focuses_username_on_mount", () => {
  it("places focus on the username input when <Register /> mounts", async () => {
    render(
      <AuthProvider>
        <Register onSwitchToLogin={() => undefined} />
      </AuthProvider>,
    );
    const u = screen.getByLabelText(/username/i);
    await waitFor(() => {
      expect(document.activeElement).toBe(u);
    });
  });
});

describe("test_web_register_form_requires_invite_code", () => {
  it("rejects submit when invite code is empty and never calls register", async () => {
    render(
      <AuthProvider>
        <Register onSwitchToLogin={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    const inviteInput = screen.getByLabelText(/invite code/i);
    expect(inviteInput).toBeRequired();

    // Force-clear the `required` attr so the browser validity check doesn't
    // shadow the explicit guard inside Register.tsx (we want both paths
    // exercised: native attr + JS-level non-empty assertion).
    inviteInput.removeAttribute("required");
    await u.type(screen.getByLabelText(/username/i), "bob");
    await u.type(screen.getByLabelText(/^password$/i), "passw0rd-placeholder");
    await u.click(screen.getByRole("button", { name: /create account/i }));

    expect(registerMock).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(/invite code/i);
  });

  it("renders validation copy on 422 without leaking the raw err.message", async () => {
    const raw = new ApiError(422, "unprocessable", "internal-validation-trace-55");
    registerMock.mockRejectedValue(raw);
    render(
      <AuthProvider>
        <Register onSwitchToLogin={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "bob");
    await u.type(screen.getByLabelText(/^password$/i), "passw0rd-placeholder");
    await u.type(screen.getByLabelText(/invite code/i), "test-invite-code-placeholder");
    await u.click(screen.getByRole("button", { name: /create account/i }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/please check the form and try again/i);
    expect(alert).not.toHaveTextContent(/internal-validation-trace-55/i);
    expect(alert).not.toHaveTextContent(/422/);
    expect(consoleErrorSpy).toHaveBeenCalledWith("Registration failed", raw);
  });

  it("renders invite-rejected copy on 401 without leaking the raw err.message", async () => {
    const raw = new ApiError(401, "unauthorized", "invite-rejected-internal-detail");
    registerMock.mockRejectedValue(raw);
    render(
      <AuthProvider>
        <Register onSwitchToLogin={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "bob");
    await u.type(screen.getByLabelText(/^password$/i), "passw0rd-placeholder");
    await u.type(screen.getByLabelText(/invite code/i), "test-invite-code-placeholder");
    await u.click(screen.getByRole("button", { name: /create account/i }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/that invite code wasn't accepted/i);
    expect(alert).not.toHaveTextContent(/that username and password don't match/i);
    expect(alert).not.toHaveTextContent(/invite-rejected-internal-detail/i);
  });

  it("calls register with invite_code when all fields are present", async () => {
    registerMock.mockResolvedValue({
      token: "test-jwt-token-placeholder",
      user: { id: "U2", username: "bob" },
    });
    render(
      <AuthProvider>
        <Register onSwitchToLogin={() => undefined} />
      </AuthProvider>,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/username/i), "bob");
    await u.type(screen.getByLabelText(/^password$/i), "passw0rd-placeholder");
    await u.type(screen.getByLabelText(/invite code/i), "test-invite-code-placeholder");
    await u.click(screen.getByRole("button", { name: /create account/i }));
    expect(registerMock).toHaveBeenCalledWith(
      "bob",
      "passw0rd-placeholder",
      "test-invite-code-placeholder",
      undefined,
    );
  });
});
