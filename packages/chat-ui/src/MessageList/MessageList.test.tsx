import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MessageList } from "./MessageList.js";
import type { ChatMessage } from "../types.js";

afterEach(() => {
  cleanup();
});

function resolveSenderEcho(id: string): string {
  return id;
}

describe("MessageList — empty / banner states", () => {
  it("renders the log container with no message rows when messages is empty", () => {
    render(<MessageList messages={[]} resolveSender={resolveSenderEcho} />);
    const list = screen.getByRole("log", { name: "conversation" });
    expect(list).toBeInTheDocument();
    expect(within(list).queryAllByTestId("msg")).toHaveLength(0);
  });

  it("renders the no-channels empty state when showNoChannelsEmpty is true", () => {
    render(
      <MessageList messages={[]} resolveSender={resolveSenderEcho} showNoChannelsEmpty={true} />,
    );
    expect(screen.getByTestId("empty-state-no-channels")).toHaveTextContent(
      /no channels available/i,
    );
  });

  it("renders the per-channel hint when showEmptyChannelHint is true and text is provided", () => {
    render(
      <MessageList
        messages={[]}
        resolveSender={resolveSenderEcho}
        showEmptyChannelHint={true}
        emptyChannelHintText="Be the first to say hello."
      />,
    );
    expect(screen.getByTestId("empty-state-channel-hint")).toHaveTextContent(
      "Be the first to say hello.",
    );
  });

  it("renders the error envelope when error is provided", () => {
    render(<MessageList messages={[]} resolveSender={resolveSenderEcho} error="boom" />);
    const alerts = screen.getAllByRole("alert");
    expect(alerts.some((el) => el.textContent === "boom")).toBe(true);
  });

  it("renders the load-older button with aria-busy when isLoadingOlder is true", () => {
    render(
      <MessageList
        messages={[]}
        resolveSender={resolveSenderEcho}
        canLoadOlder={true}
        isLoadingOlder={true}
      />,
    );
    const btn = screen.getByTestId("load-older-button");
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
  });

  it("calls onLoadOlder when the load-older button is clicked", () => {
    const onLoadOlder = vi.fn();
    render(
      <MessageList
        messages={[]}
        resolveSender={resolveSenderEcho}
        canLoadOlder={true}
        onLoadOlder={onLoadOlder}
      />,
    );
    screen.getByTestId("load-older-button").click();
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
  });
});

describe("MessageList — day-divider rendering", () => {
  it("renders one divider for a single message (anchors what day the reader is in)", () => {
    const messages: ChatMessage[] = [
      {
        id: "01H000000000000000000000A1",
        sender_user_id: "u1",
        body: "hi",
        created_at: "2026-05-07T10:00:00.000Z",
      },
    ];
    render(<MessageList messages={messages} resolveSender={resolveSenderEcho} />);
    expect(screen.getAllByTestId("day-divider")).toHaveLength(1);
  });

  it("inserts a divider between two messages crossing local midnight", () => {
    // Two messages anchored 12 hours apart. UTC offsets vary by host clock,
    // so use timestamps spaced widely enough that any offset still puts
    // them on different local days. Concretely: 2026-05-07T01:00 UTC and
    // 2026-05-08T23:00 UTC are >24h apart; for any timezone, the local
    // calendar date differs.
    const messages: ChatMessage[] = [
      {
        id: "01H000000000000000000000A1",
        sender_user_id: "u1",
        body: "first day",
        created_at: "2026-05-07T01:00:00.000Z",
      },
      {
        id: "01H000000000000000000000A2",
        sender_user_id: "u1",
        body: "second day",
        created_at: "2026-05-08T23:00:00.000Z",
      },
    ];
    render(<MessageList messages={messages} resolveSender={resolveSenderEcho} />);
    // First message always gets a divider; the cross-midnight pair adds a
    // second one before the second message.
    expect(screen.getAllByTestId("day-divider")).toHaveLength(2);
  });

  it("does not insert a divider between two messages on the same local day", () => {
    const messages: ChatMessage[] = [
      {
        id: "01H000000000000000000000A1",
        sender_user_id: "u1",
        body: "first",
        created_at: "2026-05-07T10:00:00.000Z",
      },
      {
        id: "01H000000000000000000000A2",
        sender_user_id: "u1",
        body: "second",
        created_at: "2026-05-07T10:30:00.000Z",
      },
    ];
    render(<MessageList messages={messages} resolveSender={resolveSenderEcho} />);
    // Only the leading divider, not a between-pair divider.
    expect(screen.getAllByTestId("day-divider")).toHaveLength(1);
  });

  it("suppresses the divider for an optimistic-send row whose created_at is empty", () => {
    const messages: ChatMessage[] = [
      {
        id: "01H000000000000000000000A1",
        sender_user_id: "u1",
        body: "in flight",
        created_at: "",
        status: "pending",
      },
    ];
    render(<MessageList messages={messages} resolveSender={resolveSenderEcho} />);
    expect(screen.queryAllByTestId("day-divider")).toHaveLength(0);
  });
});

describe("MessageList — self-authored aria-hidden", () => {
  it("marks own confirmed messages aria-hidden so the polite log does not echo them", () => {
    const messages: ChatMessage[] = [
      {
        id: "01H000000000000000000000A1",
        sender_user_id: "self",
        body: "i typed this",
        created_at: "2026-05-07T10:00:00.000Z",
      },
    ];
    render(<MessageList messages={messages} resolveSender={resolveSenderEcho} selfUserId="self" />);
    const article = screen.getByTestId("msg");
    expect(article).toHaveAttribute("aria-hidden", "true");
  });

  it("keeps own failed messages announceable so SR users hear about send failures", () => {
    const messages: ChatMessage[] = [
      {
        id: "01H000000000000000000000A1",
        sender_user_id: "self",
        body: "didn't go through",
        created_at: "2026-05-07T10:00:00.000Z",
        status: "failed",
      },
    ];
    render(<MessageList messages={messages} resolveSender={resolveSenderEcho} selfUserId="self" />);
    const article = screen.getByTestId("msg");
    expect(article).not.toHaveAttribute("aria-hidden");
  });
});
