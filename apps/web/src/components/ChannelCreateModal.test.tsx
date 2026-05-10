import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError, type Channel } from "@hackathon/api-client";

import { ChannelCreateModal } from "./ChannelCreateModal.js";

afterEach(() => {
  cleanup();
  document.body.style.overflow = "";
});

function makeCh(name: string): Channel {
  return { id: `id-${name}`, name, created_at: "2026-01-01T00:00:00Z" };
}

describe("ChannelCreateModal", () => {
  it("shows the helper text and disables Submit until the input matches the regex", async () => {
    const onCreate = vi.fn();
    render(<ChannelCreateModal open={true} onClose={vi.fn()} onCreate={onCreate} />);
    expect(screen.getByText(/lowercase letters, digits, hyphens/i)).toBeInTheDocument();
    const submit = screen.getByRole("button", { name: /^create$/i });
    expect(submit).toBeDisabled();

    const u = userEvent.setup();
    const input = screen.getByLabelText(/channel name/i);
    await u.type(input, "Books"); // uppercase: still invalid
    expect(submit).toBeDisabled();

    await u.clear(input);
    await u.type(input, "books");
    expect(submit).toBeEnabled();
  });

  it("calls onCreate, closes the modal, and notifies onCreated on success", async () => {
    const created = makeCh("books");
    const onCreate = vi.fn().mockResolvedValue(created);
    const onClose = vi.fn();
    const onCreated = vi.fn();
    render(
      <ChannelCreateModal
        open={true}
        onClose={onClose}
        onCreate={onCreate}
        onCreated={onCreated}
      />,
    );
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/channel name/i), "books");
    await u.click(screen.getByRole("button", { name: /^create$/i }));
    await waitFor(() => {
      expect(onCreate).toHaveBeenCalledWith("books", { isPublic: false });
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCreated).toHaveBeenCalledWith(created);
  });

  it("renders a server ApiError inline and keeps the modal open", async () => {
    const onCreate = vi
      .fn()
      .mockRejectedValueOnce(new ApiError(409, "channel_name_taken", "Channel name already taken"));
    const onClose = vi.fn();
    render(<ChannelCreateModal open={true} onClose={onClose} onCreate={onCreate} />);
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/channel name/i), "general");
    await u.click(screen.getByRole("button", { name: /^create$/i }));
    await screen.findByText(/already taken/i);
    expect(onClose).not.toHaveBeenCalled();
    // Re-enabled so the user can correct + retry.
    expect(screen.getByRole("button", { name: /^create$/i })).toBeEnabled();
  });

  it("shows a Creating… label while the request is in flight", async () => {
    let resolveCreate: ((c: Channel) => void) | undefined;
    const onCreate = vi.fn(
      () =>
        new Promise<Channel>((resolve) => {
          resolveCreate = resolve;
        }),
    );
    render(<ChannelCreateModal open={true} onClose={vi.fn()} onCreate={onCreate} />);
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/channel name/i), "books");
    await u.click(screen.getByRole("button", { name: /^create$/i }));
    await screen.findByRole("button", { name: /creating…/i });

    // Drain to avoid leaking a pending promise into the next test.
    resolveCreate?.(makeCh("books"));
    await waitFor(() => {
      expect(onCreate).toHaveBeenCalled();
    });
  });

  it("renders a generic network message when the error has no ApiError shape", async () => {
    const onCreate = vi.fn().mockRejectedValueOnce(new TypeError("Failed to fetch"));
    render(<ChannelCreateModal open={true} onClose={vi.fn()} onCreate={onCreate} />);
    const u = userEvent.setup();
    await u.type(screen.getByLabelText(/channel name/i), "books");
    await u.click(screen.getByRole("button", { name: /^create$/i }));
    await screen.findByText(/could not reach the server/i);
  });
});
