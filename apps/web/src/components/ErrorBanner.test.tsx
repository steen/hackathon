import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const dismissAuthErrorMock = vi.fn();
let authErrorValue: string | null = null;

vi.mock("../auth/AuthContext.js", () => ({
  useAuth: () => ({
    token: null,
    user: null,
    loading: false,
    error: authErrorValue,
    login: vi.fn(),
    register: vi.fn(),
    logout: vi.fn(),
    dismissError: dismissAuthErrorMock,
  }),
}));

import { ErrorBanner } from "./ErrorBanner.js";
import {
  _resetAppErrorSinkForTests,
  dismissAppError,
  reportAppError,
} from "../lib/userFacingError.js";

const dismissAppErrorSpy = vi.fn();
vi.mock("../lib/userFacingError.js", async () => {
  const actual = await vi.importActual<typeof import("../lib/userFacingError.js")>(
    "../lib/userFacingError.js",
  );
  return {
    ...actual,
    dismissAppError: (): void => {
      dismissAppErrorSpy();
      actual.dismissAppError();
    },
  };
});

beforeEach(() => {
  authErrorValue = null;
  dismissAuthErrorMock.mockReset();
  dismissAppErrorSpy.mockReset();
  _resetAppErrorSinkForTests();
});

afterEach(() => {
  cleanup();
});

describe("test_web_error_banner_two_source_rendering", () => {
  it("renders nothing when both sources are null", () => {
    render(<ErrorBanner />);
    expect(screen.queryByTestId("auth-error-banner")).not.toBeInTheDocument();
  });

  it("renders the auth message when only AuthContext.error is set", () => {
    authErrorValue = "auth-source-message";
    render(<ErrorBanner />);
    const banner = screen.getByTestId("auth-error-banner");
    expect(banner).toHaveTextContent("auth-source-message");
  });

  it("renders the sink message when only the sink is set", () => {
    render(<ErrorBanner />);
    act(() => {
      reportAppError("sink-source-message");
    });
    const banner = screen.getByTestId("auth-error-banner");
    expect(banner).toHaveTextContent("sink-source-message");
  });

  it("renders the auth message (precedence) when both are set", () => {
    authErrorValue = "auth-source-message";
    render(<ErrorBanner />);
    act(() => {
      reportAppError("sink-source-message");
    });
    const banner = screen.getByTestId("auth-error-banner");
    expect(banner).toHaveTextContent("auth-source-message");
    expect(banner).not.toHaveTextContent("sink-source-message");
  });
});

describe("test_web_error_banner_dismiss_routing", () => {
  it("clicking Dismiss with auth active calls dismissError from AuthContext (not dismissAppError)", async () => {
    authErrorValue = "auth-source-message";
    render(<ErrorBanner />);

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /dismiss error/i }));

    expect(dismissAuthErrorMock).toHaveBeenCalledTimes(1);
    expect(dismissAppErrorSpy).not.toHaveBeenCalled();
  });

  it("clicking Dismiss with sink-only active calls dismissAppError (not dismissError)", async () => {
    render(<ErrorBanner />);
    act(() => {
      reportAppError("sink-source-message");
    });

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /dismiss error/i }));

    expect(dismissAppErrorSpy).toHaveBeenCalledTimes(1);
    expect(dismissAuthErrorMock).not.toHaveBeenCalled();
  });

  it("clicking Dismiss with both sources active routes to dismissError (auth wins)", async () => {
    authErrorValue = "auth-source-message";
    render(<ErrorBanner />);
    act(() => {
      reportAppError("sink-source-message");
    });

    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /dismiss error/i }));

    expect(dismissAuthErrorMock).toHaveBeenCalledTimes(1);
    expect(dismissAppErrorSpy).not.toHaveBeenCalled();
  });
});

describe("test_web_error_banner_sink_falls_through_after_auth_dismiss", () => {
  it("sink message becomes visible after the auth source clears, if sink is still set", () => {
    authErrorValue = "auth-source-message";
    const { rerender } = render(<ErrorBanner />);
    act(() => {
      reportAppError("sink-source-message");
    });
    expect(screen.getByTestId("auth-error-banner")).toHaveTextContent("auth-source-message");

    authErrorValue = null;
    rerender(<ErrorBanner />);

    const banner = screen.getByTestId("auth-error-banner");
    expect(banner).toHaveTextContent("sink-source-message");
    expect(banner).not.toHaveTextContent("auth-source-message");
  });

  it("nothing remains visible if the sink is also dismissed after auth clears", () => {
    authErrorValue = "auth-source-message";
    const { rerender } = render(<ErrorBanner />);
    act(() => {
      reportAppError("sink-source-message");
    });

    authErrorValue = null;
    rerender(<ErrorBanner />);
    expect(screen.getByTestId("auth-error-banner")).toHaveTextContent("sink-source-message");

    act(() => {
      dismissAppError();
    });

    expect(screen.queryByTestId("auth-error-banner")).not.toBeInTheDocument();
  });
});
