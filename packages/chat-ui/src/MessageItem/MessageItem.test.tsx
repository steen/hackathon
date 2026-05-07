import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MessageItem } from "./MessageItem.js";
import { userColor } from "../colorize.js";

afterEach(() => {
  cleanup();
});

describe("MessageItem — status rendering", () => {
  it("pending: shows the 'Sending…' status badge and suppresses the timestamp", () => {
    const { container } = render(
      <MessageItem
        sender="alice"
        body="hello"
        createdAt="2026-05-07T10:00:00.000Z"
        status="pending"
      />,
    );
    const badge = screen.getByRole("status");
    expect(badge).toHaveTextContent(/sending/i);
    expect(container.querySelector("time")).toBeNull();
    expect(screen.getByTestId("msg")).toHaveAttribute("data-status", "pending");
  });

  it("failed: shows the failed badge, reason text, and a Retry button wired to onRetry", () => {
    const onRetry = vi.fn();
    render(
      <MessageItem
        sender="alice"
        body="hello"
        createdAt="2026-05-07T10:00:00.000Z"
        status="failed"
        failureReason="rate limited"
        reasonId="msg-failed-reason-test"
        onRetry={onRetry}
      />,
    );
    expect(screen.getByTestId("msg-failed-badge")).toHaveTextContent(/failed to send/i);
    expect(screen.getByTestId("msg-failed-reason")).toHaveTextContent("rate limited");

    const retry = screen.getByRole("button", { name: /retry/i });
    fireEvent.click(retry);
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("msg")).toHaveAttribute("data-status", "failed");
  });

  it("confirmed: renders a <time> with dateTime equal to the raw ISO and no badges", () => {
    const iso = "2026-05-07T10:00:00.000Z";
    const { container } = render(<MessageItem sender="alice" body="hello" createdAt={iso} />);
    const time = container.querySelector("time");
    expect(time).not.toBeNull();
    expect(time).toHaveAttribute("datetime", iso);
    expect(screen.queryByTestId("msg-failed-badge")).toBeNull();
    expect(screen.queryByRole("status")).toBeNull();
    expect(screen.getByTestId("msg")).toHaveAttribute("data-status", "sent");
  });
});

describe("MessageItem — sender colorization", () => {
  it("applies the same color to the same sender name across renders (deterministic)", () => {
    const a = render(<MessageItem sender="alice" body="x" createdAt="2026-05-07T10:00:00.000Z" />);
    const senderA = a.container.querySelector(".msg__sender");
    const colorA = senderA?.getAttribute("style") ?? "";
    a.unmount();

    const b = render(<MessageItem sender="alice" body="y" createdAt="2026-05-07T10:00:00.000Z" />);
    const senderB = b.container.querySelector(".msg__sender");
    const colorB = senderB?.getAttribute("style") ?? "";

    expect(colorA.length).toBeGreaterThan(0);
    expect(colorA).toBe(colorB);
  });

  it("gives different sender names different colors (no shared palette collision)", () => {
    const a = render(<MessageItem sender="alice" body="x" createdAt="2026-05-07T10:00:00.000Z" />);
    const colorA = a.container.querySelector(".msg__sender")?.getAttribute("style") ?? "";
    a.unmount();

    const b = render(<MessageItem sender="bob" body="x" createdAt="2026-05-07T10:00:00.000Z" />);
    const colorB = b.container.querySelector(".msg__sender")?.getAttribute("style") ?? "";

    expect(colorA).not.toBe(colorB);
  });

  it("renders an oklch() inline color whose hue matches userColor()'s formula", () => {
    const expected = userColor("carol");
    // userColor returns `oklch(78% 0.15 <hue>)`; jsdom normalizes the
    // percentage to a 0..1 fraction (`0.78`) but preserves the hue value.
    // Extract the hue from the canonical form and assert the rendered
    // style carries the same hue, which is the determinism signal we
    // care about — the exact percentage formatting is jsdom's choice.
    const hueMatch = /oklch\(\s*78%\s+0\.15\s+(\d+)\s*\)/.exec(expected);
    expect(hueMatch).not.toBeNull();
    const hue = hueMatch?.[1] ?? "";

    render(<MessageItem sender="carol" body="x" createdAt="2026-05-07T10:00:00.000Z" />);
    const sender = screen.getByText("carol");
    const style = sender.getAttribute("style") ?? "";
    expect(style).toContain("oklch(");
    expect(style).toContain(hue);
  });
});

describe("MessageItem — aria-hidden plumbing", () => {
  it("renders aria-hidden=true when the consumer passes ariaHidden", () => {
    render(
      <MessageItem
        sender="alice"
        body="hi"
        createdAt="2026-05-07T10:00:00.000Z"
        ariaHidden={true}
      />,
    );
    expect(screen.getByTestId("msg")).toHaveAttribute("aria-hidden", "true");
  });
});
