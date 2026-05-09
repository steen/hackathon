import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ChannelsList } from "./ChannelsList.js";

afterEach(() => {
  cleanup();
});

describe("ChannelsList — unread badges", () => {
  it("renders no badge when unread_count is 0 or undefined", () => {
    render(
      <ChannelsList
        channels={[
          { id: "C1", name: "general" },
          { id: "C2", name: "books", unread_count: 0 },
        ]}
        activeId="C1"
        onSelect={() => undefined}
      />,
    );
    expect(screen.queryAllByTestId("channel-unread-badge")).toHaveLength(0);
  });

  it("renders the count when unread_count > 0", () => {
    render(
      <ChannelsList
        channels={[
          { id: "C1", name: "general", unread_count: 0 },
          { id: "C2", name: "books", unread_count: 3 },
        ]}
        activeId="C1"
        onSelect={() => undefined}
      />,
    );
    const badges = screen.getAllByTestId("channel-unread-badge");
    expect(badges).toHaveLength(1);
    expect(badges[0]).toHaveTextContent("3");
  });

  it("caps the displayed count at 99+ but exposes the exact number to SR via aria-label", () => {
    render(
      <ChannelsList
        channels={[{ id: "C1", name: "general", unread_count: 250 }]}
        activeId={null}
        onSelect={() => undefined}
      />,
    );
    expect(screen.getByTestId("channel-unread-badge")).toHaveTextContent("99+");
    expect(screen.getByRole("button", { name: /250 unread/i })).toBeInTheDocument();
  });

  it("calls onSelect with the channel id when a row is clicked", () => {
    const onSelect = vi.fn();
    render(
      <ChannelsList
        channels={[{ id: "C2", name: "books", unread_count: 4 }]}
        activeId={null}
        onSelect={onSelect}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /books, 4 unread/i }));
    expect(onSelect).toHaveBeenCalledWith("C2");
  });

  it("active channel still wins aria-current with a badge present", () => {
    render(
      <ChannelsList
        channels={[{ id: "C1", name: "general", unread_count: 2 }]}
        activeId="C1"
        onSelect={() => undefined}
      />,
    );
    expect(screen.getByRole("button", { name: /general, 2 unread/i })).toHaveAttribute(
      "aria-current",
      "true",
    );
  });
});
