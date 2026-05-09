import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Conversation } from "@hackathon/api-client";
import { DMSidebar } from "./DMSidebar.js";

afterEach(cleanup);

function makeConv(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: "DM1",
    user_a_id: "U1",
    user_b_id: "U2",
    last_message_id: "M1",
    last_message_at: "2026-01-02T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    peer: { id: "U2", username: "bob" },
    unread_count: 0,
    ...overrides,
  };
}

describe("DMSidebar", () => {
  it("renders one row per conversation labeled by the peer username", () => {
    render(
      <DMSidebar
        conversations={[
          makeConv({ id: "DM1", peer: { id: "U2", username: "bob" } }),
          makeConv({ id: "DM2", peer: { id: "U3", username: "carol" } }),
        ]}
        activeId={null}
        onSelect={vi.fn()}
        onNew={vi.fn()}
      />,
    );
    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(screen.getByText("bob")).toBeInTheDocument();
    expect(screen.getByText("carol")).toBeInTheDocument();
  });

  it("renders per-row unread badge only when unread_count > 0", () => {
    render(
      <DMSidebar
        conversations={[
          makeConv({ id: "DM1", peer: { id: "U2", username: "bob" }, unread_count: 0 }),
          makeConv({ id: "DM2", peer: { id: "U3", username: "carol" }, unread_count: 4 }),
        ]}
        activeId={null}
        onSelect={vi.fn()}
        onNew={vi.fn()}
      />,
    );
    const badges = screen.getAllByTestId("dm-unread-badge");
    expect(badges).toHaveLength(1);
    expect(badges[0]?.textContent).toBe("4");
  });

  it("aggregate header badge sums unread across rows; hidden when total is 0", () => {
    const { rerender } = render(
      <DMSidebar
        conversations={[
          makeConv({ id: "DM1", peer: { id: "U2", username: "bob" }, unread_count: 2 }),
          makeConv({ id: "DM2", peer: { id: "U3", username: "carol" }, unread_count: 5 }),
        ]}
        activeId={null}
        onSelect={vi.fn()}
        onNew={vi.fn()}
      />,
    );
    expect(screen.getByTestId("dm-aggregate-unread-badge").textContent).toBe("7");

    rerender(
      <DMSidebar
        conversations={[
          makeConv({ id: "DM1", peer: { id: "U2", username: "bob" }, unread_count: 0 }),
        ]}
        activeId={null}
        onSelect={vi.fn()}
        onNew={vi.fn()}
      />,
    );
    expect(screen.queryByTestId("dm-aggregate-unread-badge")).toBeNull();
  });

  it("caps badge text at 99+ for large counts", () => {
    render(
      <DMSidebar
        conversations={[
          makeConv({ id: "DM1", peer: { id: "U2", username: "bob" }, unread_count: 250 }),
        ]}
        activeId={null}
        onSelect={vi.fn()}
        onNew={vi.fn()}
      />,
    );
    expect(screen.getByTestId("dm-unread-badge").textContent).toBe("99+");
    expect(screen.getByTestId("dm-aggregate-unread-badge").textContent).toBe("99+");
  });

  it("calls onSelect with the conversation id on row click", async () => {
    const onSelect = vi.fn();
    render(
      <DMSidebar
        conversations={[makeConv({ id: "DM1", peer: { id: "U2", username: "bob" } })]}
        activeId={null}
        onSelect={onSelect}
        onNew={vi.fn()}
      />,
    );
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /^bob$/ }));
    expect(onSelect).toHaveBeenCalledWith("DM1");
  });

  it("calls onNew when the + New DM button is clicked", async () => {
    const onNew = vi.fn();
    render(<DMSidebar conversations={[]} activeId={null} onSelect={vi.fn()} onNew={onNew} />);
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /start new dm/i }));
    expect(onNew).toHaveBeenCalledTimes(1);
  });

  it("marks the active row with aria-current=true", () => {
    render(
      <DMSidebar
        conversations={[
          makeConv({ id: "DM1", peer: { id: "U2", username: "bob" } }),
          makeConv({ id: "DM2", peer: { id: "U3", username: "carol" } }),
        ]}
        activeId="DM2"
        onSelect={vi.fn()}
        onNew={vi.fn()}
      />,
    );
    const carolRow = screen.getByRole("button", { name: /^carol$/ });
    expect(carolRow.getAttribute("aria-current")).toBe("true");
    const bobRow = screen.getByRole("button", { name: /^bob$/ });
    expect(bobRow.getAttribute("aria-current")).toBeNull();
  });
});
