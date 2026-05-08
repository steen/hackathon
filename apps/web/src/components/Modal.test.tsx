import { afterEach, describe, expect, it, vi } from "vitest";
import { useRef, useState } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { Modal } from "./Modal.js";

afterEach(() => {
  cleanup();
  // Defensive: prevent body-overflow leakage between tests if a render
  // unmount didn't run a cleanup effect (e.g. mid-test cleanup() above).
  document.body.style.overflow = "";
});

describe("test_web_modal_render_gating", () => {
  it("renders nothing when open=false", () => {
    render(
      <Modal open={false} onClose={vi.fn()} title="Hidden">
        <p>body-text</p>
      </Modal>,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("body-text")).not.toBeInTheDocument();
  });

  it("renders title, body, dialog role and aria-labelledby when open=true", () => {
    render(
      <Modal open={true} onClose={vi.fn()} title="Visible Title">
        <p>body-text</p>
      </Modal>,
    );
    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveAttribute("aria-modal", "true");
    const heading = screen.getByRole("heading", { level: 2, name: "Visible Title" });
    expect(dialog.getAttribute("aria-labelledby")).toBe(heading.id);
    expect(screen.getByText("body-text")).toBeInTheDocument();
  });
});

describe("test_web_modal_portal", () => {
  it("mounts the dialog into document.body, not the React tree's parent", () => {
    const { container } = render(
      <Modal open={true} onClose={vi.fn()} title="Portal">
        <p>portal-body</p>
      </Modal>,
    );
    // The Testing Library container is the React tree's parent. With a
    // portal, the dialog is NOT inside it.
    expect(container.querySelector("[role='dialog']")).toBeNull();
    expect(container.querySelector("[data-testid='modal-backdrop']")).toBeNull();
    // The dialog IS in document.body (portal target).
    const dialog = screen.getByRole("dialog");
    expect(document.body.contains(dialog)).toBe(true);
  });
});

describe("test_web_modal_close_triggers", () => {
  it("calls onClose when Escape is pressed", async () => {
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="T">
        <button type="button">inner</button>
      </Modal>,
    );
    const u = userEvent.setup();
    await u.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("calls onClose on backdrop pointerdown", () => {
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="T">
        <button type="button">inner</button>
      </Modal>,
    );
    fireEvent.pointerDown(screen.getByTestId("modal-backdrop"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not call onClose on panel pointerdown", () => {
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="T">
        <button type="button">inner</button>
      </Modal>,
    );
    fireEvent.pointerDown(screen.getByRole("dialog"));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("does not dismiss when pointerdown starts on the panel and pointerup lands on the backdrop", () => {
    // Spec scenario: a user selecting text inside the panel and dragging
    // out onto the backdrop must NOT dismiss. With pointerdown semantics
    // the down-event on the panel stops propagation, and a release-only
    // pointerup on the backdrop does not fire the close handler.
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="T">
        <button type="button">inner</button>
      </Modal>,
    );
    const panel = screen.getByRole("dialog");
    const backdrop = screen.getByTestId("modal-backdrop");
    fireEvent.pointerDown(panel);
    fireEvent.pointerUp(backdrop);
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("test_web_modal_focus_management", () => {
  it("focuses the first focusable element on open", () => {
    render(
      <Modal open={true} onClose={vi.fn()} title="T">
        <button type="button">first-btn</button>
        <button type="button">second-btn</button>
      </Modal>,
    );
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "first-btn" }));
  });

  it("restores focus to the trigger element when the modal closes", async () => {
    function Harness(): React.JSX.Element {
      const [open, setOpen] = useState(false);
      return (
        <>
          <button
            type="button"
            data-testid="trigger"
            onClick={() => {
              setOpen(true);
            }}
          >
            open
          </button>
          <Modal
            open={open}
            onClose={() => {
              setOpen(false);
            }}
            title="T"
          >
            <button type="button">inner</button>
          </Modal>
        </>
      );
    }
    render(<Harness />);
    const trigger = screen.getByTestId("trigger");
    trigger.focus();
    expect(document.activeElement).toBe(trigger);

    const u = userEvent.setup();
    await u.click(trigger);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "inner" }));

    await u.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.activeElement).toBe(trigger);
  });
});

describe("test_web_modal_focus_trap", () => {
  it("Tab from the last focusable element wraps focus to the first", () => {
    render(
      <Modal open={true} onClose={vi.fn()} title="T">
        <button type="button">first-btn</button>
        <textarea aria-label="middle-area" />
        <button type="button">last-btn</button>
      </Modal>,
    );
    const first = screen.getByRole("button", { name: "first-btn" });
    const last = screen.getByRole("button", { name: "last-btn" });
    last.focus();
    expect(document.activeElement).toBe(last);
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(first);
  });

  it("Shift-Tab from the first focusable element wraps focus to the last", () => {
    render(
      <Modal open={true} onClose={vi.fn()} title="T">
        <button type="button">first-btn</button>
        <textarea aria-label="middle-area" />
        <button type="button">last-btn</button>
      </Modal>,
    );
    const first = screen.getByRole("button", { name: "first-btn" });
    const last = screen.getByRole("button", { name: "last-btn" });
    // Modal auto-focuses the first focusable on open.
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it("Tab from outside the panel snaps focus back inside", () => {
    function Harness(): React.JSX.Element {
      return (
        <>
          <button type="button" data-testid="outside">
            outside
          </button>
          <Modal open={true} onClose={vi.fn()} title="T">
            <button type="button">first-btn</button>
            <button type="button">last-btn</button>
          </Modal>
        </>
      );
    }
    render(<Harness />);
    const outside = screen.getByTestId("outside");
    const first = screen.getByRole("button", { name: "first-btn" });
    outside.focus();
    expect(document.activeElement).toBe(outside);
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(first);
  });
});

describe("test_web_modal_body_scroll_lock", () => {
  it("toggles document.body.style.overflow on open and restores on close", () => {
    document.body.style.overflow = "auto";
    function Harness({ open }: { open: boolean }): React.JSX.Element {
      return (
        <Modal open={open} onClose={vi.fn()} title="T">
          <button type="button">inner</button>
        </Modal>
      );
    }
    const { rerender } = render(<Harness open={false} />);
    expect(document.body.style.overflow).toBe("auto");

    act(() => {
      rerender(<Harness open={true} />);
    });
    expect(document.body.style.overflow).toBe("hidden");

    act(() => {
      rerender(<Harness open={false} />);
    });
    expect(document.body.style.overflow).toBe("auto");
  });
});

describe("test_web_modal_close_on_backdrop_prop", () => {
  it("closeOnBackdrop is true by default — backdrop pointerdown closes", () => {
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="T">
        <button type="button">inner</button>
      </Modal>,
    );
    fireEvent.pointerDown(screen.getByTestId("modal-backdrop"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("closeOnBackdrop=false suppresses onClose on backdrop pointerdown but Escape still closes", async () => {
    const onClose = vi.fn();
    render(
      <Modal open={true} onClose={onClose} title="T" closeOnBackdrop={false}>
        <button type="button">inner</button>
      </Modal>,
    );
    fireEvent.pointerDown(screen.getByTestId("modal-backdrop"));
    expect(onClose).not.toHaveBeenCalled();

    const u = userEvent.setup();
    await u.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("test_web_modal_initial_focus_ref", () => {
  it("focuses initialFocusRef target on open instead of the first focusable", () => {
    function Harness(): React.JSX.Element {
      const ref = useRef<HTMLButtonElement | null>(null);
      return (
        <Modal open={true} onClose={vi.fn()} title="T" initialFocusRef={ref}>
          <button type="button">first-btn</button>
          <button ref={ref} type="button">
            second-btn
          </button>
        </Modal>
      );
    }
    render(<Harness />);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "second-btn" }));
  });

  it("falls back to first-focusable when initialFocusRef is unset", () => {
    render(
      <Modal open={true} onClose={vi.fn()} title="T">
        <button type="button">first-btn</button>
        <button type="button">second-btn</button>
      </Modal>,
    );
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "first-btn" }));
  });
});
