import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError, type Channel } from "@hackathon/api-client";

import { ChannelRenameModal } from "./ChannelRenameModal.js";

afterEach(() => {
  cleanup();
  document.body.style.overflow = "";
});

function makeCh(name: string): Channel {
  return { id: "C2", name, created_at: "2026-01-01T00:00:00Z" };
}

describe("ChannelRenameModal", () => {
  it("pre-fills the input with the current name", () => {
    render(
      <ChannelRenameModal
        open={true}
        onClose={vi.fn()}
        channelId="C2"
        currentName="books"
        onRename={vi.fn()}
      />,
    );
    expect(screen.getByLabelText(/new name/i)).toHaveValue("books");
  });

  it("disables Submit when the input is unchanged or invalid", async () => {
    render(
      <ChannelRenameModal
        open={true}
        onClose={vi.fn()}
        channelId="C2"
        currentName="books"
        onRename={vi.fn()}
      />,
    );
    const u = userEvent.setup();
    const input = screen.getByLabelText(/new name/i);
    const submit = screen.getByRole("button", { name: /^rename$/i });
    // Unchanged name is allowed by the regex; the button stays enabled
    // but a submit short-circuits to a no-op close (covered in the close
    // test below). Toggle to an invalid value to exercise the disabled
    // path.
    await u.clear(input);
    await u.type(input, "Bad Name");
    expect(submit).toBeDisabled();
  });

  it("calls onRename and closes on success", async () => {
    const onRename = vi.fn().mockResolvedValue(makeCh("reading"));
    const onClose = vi.fn();
    render(
      <ChannelRenameModal
        open={true}
        onClose={onClose}
        channelId="C2"
        currentName="books"
        onRename={onRename}
      />,
    );
    const u = userEvent.setup();
    const input = screen.getByLabelText(/new name/i);
    await u.clear(input);
    await u.type(input, "reading");
    await u.click(screen.getByRole("button", { name: /^rename$/i }));
    await waitFor(() => {
      expect(onRename).toHaveBeenCalledWith("C2", "reading");
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows the server's 409 message inline and keeps the modal open", async () => {
    const onRename = vi
      .fn()
      .mockRejectedValueOnce(new ApiError(409, "channel_name_taken", "Channel name already taken"));
    const onClose = vi.fn();
    render(
      <ChannelRenameModal
        open={true}
        onClose={onClose}
        channelId="C2"
        currentName="books"
        onRename={onRename}
      />,
    );
    const u = userEvent.setup();
    const input = screen.getByLabelText(/new name/i);
    await u.clear(input);
    await u.type(input, "general");
    await u.click(screen.getByRole("button", { name: /^rename$/i }));
    await screen.findByText(/already taken/i);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("shows a Renaming… label while the request is in flight", async () => {
    let resolveRename: ((c: Channel) => void) | undefined;
    const onRename = vi.fn(
      () =>
        new Promise<Channel>((resolve) => {
          resolveRename = resolve;
        }),
    );
    render(
      <ChannelRenameModal
        open={true}
        onClose={vi.fn()}
        channelId="C2"
        currentName="books"
        onRename={onRename}
      />,
    );
    const u = userEvent.setup();
    const input = screen.getByLabelText(/new name/i);
    await u.clear(input);
    await u.type(input, "reading");
    await u.click(screen.getByRole("button", { name: /^rename$/i }));
    await screen.findByRole("button", { name: /renaming…/i });

    resolveRename?.(makeCh("reading"));
    await waitFor(() => {
      expect(onRename).toHaveBeenCalled();
    });
  });

  it("submitting an unchanged name is a no-op close (no API call)", async () => {
    const onRename = vi.fn();
    const onClose = vi.fn();
    render(
      <ChannelRenameModal
        open={true}
        onClose={onClose}
        channelId="C2"
        currentName="books"
        onRename={onRename}
      />,
    );
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /^rename$/i }));
    expect(onRename).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
