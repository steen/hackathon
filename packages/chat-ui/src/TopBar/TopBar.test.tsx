import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { TopBar } from "./TopBar.js";

afterEach(() => {
  cleanup();
});

const aliceUser = { id: "01H000000000000000000000A1", username: "alice" };

describe("TopBar — consolidated identity surface", () => {
  it("renders workspace name and user name in the same banner", () => {
    render(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={true} onSignOut={vi.fn()} />,
    );
    const banner = screen.getByRole("banner");
    expect(banner).toHaveTextContent("hackathon-dev");
    expect(banner).toHaveTextContent("alice");
  });

  it("renders exactly one role=status element (single source of truth for connection)", () => {
    render(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={true} onSignOut={vi.fn()} />,
    );
    expect(screen.getAllByRole("status")).toHaveLength(1);
  });

  it("Sign-out button is wired to the onSignOut prop", () => {
    const onSignOut = vi.fn();
    render(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={true} onSignOut={onSignOut} />,
    );
    fireEvent.click(screen.getByRole("button", { name: /sign out/i }));
    expect(onSignOut).toHaveBeenCalledTimes(1);
  });
});

describe("TopBar — connection status surface", () => {
  it("online=true announces 'online' in the polite live region", () => {
    render(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={true} onSignOut={vi.fn()} />,
    );
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("online");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status.className).toContain("top-bar__status-dot--online");
  });

  it("online=false announces 'offline' and swaps the dot modifier class", () => {
    render(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={false} onSignOut={vi.fn()} />,
    );
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("offline");
    expect(status.className).toContain("top-bar__status-dot--offline");
  });

  it("rerendering with a flipped online prop swaps the announced status text", () => {
    const { rerender } = render(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={false} onSignOut={vi.fn()} />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("offline");

    rerender(
      <TopBar workspaceName="hackathon-dev" user={aliceUser} online={true} onSignOut={vi.fn()} />,
    );
    expect(screen.getByRole("status")).toHaveTextContent("online");
  });
});

describe("TopBar — username display", () => {
  it("renders whatever username string the consumer resolved (offline-fallback contract is upstream)", () => {
    // The username-resolution fallback is the consumer's job (apps/web's
    // useMe/useAuth wiring); TopBar's contract is to render the string it's
    // given. Test that contract directly.
    render(
      <TopBar
        workspaceName="hackathon-dev"
        user={{ id: "01H000000000000000000000A2", username: "user-fallback-id" }}
        online={false}
        onSignOut={vi.fn()}
      />,
    );
    expect(screen.getByText("user-fallback-id")).toBeInTheDocument();
  });
});
