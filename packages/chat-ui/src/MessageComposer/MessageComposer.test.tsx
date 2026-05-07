import type * as React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { MessageComposer } from "./MessageComposer.js";

afterEach(() => {
  cleanup();
});

interface HarnessProps {
  onSubmit: () => void;
  initial?: string;
  maxBytes?: number;
  disabled?: boolean;
}

// Wrapper that owns `value` so onChange behaves the way the real consumer
// (apps/web Chat route) wires it. Bare `value` props can't be edited via
// fireEvent.change on a controlled input without a state holder.
function Harness({
  onSubmit,
  initial = "",
  maxBytes = 4096,
  disabled = false,
}: HarnessProps): React.JSX.Element {
  const [value, setValue] = useState(initial);
  return (
    <MessageComposer
      value={value}
      onChange={setValue}
      onSubmit={onSubmit}
      maxBytes={maxBytes}
      disabled={disabled}
    />
  );
}

describe("MessageComposer — Enter and Shift+Enter handling", () => {
  it("Enter without Shift fires onSubmit", () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} initial="hello" />);
    const ta = screen.getByTestId("composer-textarea");
    fireEvent.keyDown(ta, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("Shift+Enter does NOT fire onSubmit (newline insertion is the browser's job)", () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} initial="line one" />);
    const ta = screen.getByTestId("composer-textarea");
    fireEvent.keyDown(ta, { key: "Enter", shiftKey: true });
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe("MessageComposer — IME composition", () => {
  it("Enter during compositionstart..compositionend (candidate commit) does NOT submit", () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} initial="draft" />);
    const ta = screen.getByTestId("composer-textarea");

    fireEvent.compositionStart(ta);
    fireEvent.keyDown(ta, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();

    fireEvent.compositionEnd(ta);
    fireEvent.keyDown(ta, { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it("nativeEvent.isComposing=true also blocks submission (browsers vary on which signal arrives)", () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} initial="draft" />);
    const ta = screen.getByTestId("composer-textarea");
    fireEvent.keyDown(ta, { key: "Enter", isComposing: true });
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe("MessageComposer — byte-limit warning + send-button gating", () => {
  it("counter is hidden well below the warn threshold", () => {
    render(<Harness onSubmit={vi.fn()} initial="" />);
    expect(screen.queryByTestId("composer-counter")).toBeNull();

    cleanup();
    render(<Harness onSubmit={vi.fn()} initial={"x".repeat(100)} />);
    expect(screen.queryByTestId("composer-counter")).toBeNull();
  });

  it("counter appears at the 80% warn threshold (3277 / 4096 bytes)", () => {
    render(<Harness onSubmit={vi.fn()} initial={"x".repeat(3277)} />);
    const counter = screen.getByTestId("composer-counter");
    expect(counter).toHaveClass("composer__counter--warn");
    expect(counter.textContent).toContain("3277");
    expect(counter.textContent).toContain("4096");
  });

  it("over-cap state flips counter to error class and disables Send", () => {
    render(<Harness onSubmit={vi.fn()} initial={"x".repeat(4097)} />);
    const counter = screen.getByTestId("composer-counter");
    expect(counter).toHaveClass("composer__counter--error");
    expect(counter.textContent).toContain("too long to send");
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("Send button is disabled when value is empty", () => {
    render(<Harness onSubmit={vi.fn()} initial="" />);
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("Send button is disabled when value is whitespace-only", () => {
    render(<Harness onSubmit={vi.fn()} initial={"   \n  "} />);
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("Enter while over-cap does not fire onSubmit", () => {
    const onSubmit = vi.fn();
    render(<Harness onSubmit={onSubmit} initial={"x".repeat(4097)} />);
    const ta = screen.getByTestId("composer-textarea");
    fireEvent.keyDown(ta, { key: "Enter" });
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
