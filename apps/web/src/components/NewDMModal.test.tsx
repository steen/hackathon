import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError, type Conversation } from "@hackathon/api-client";

const httpRequestMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: { request: httpRequestMock },
  }),
}));

import { NewDMModal } from "./NewDMModal.js";

beforeEach(() => {
  document.body.style.overflow = "";
});

afterEach(() => {
  cleanup();
  document.body.style.overflow = "";
  httpRequestMock.mockReset();
});

function makeConv(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: "DM-new",
    user_a_id: "U1",
    user_b_id: "U2",
    last_message_id: null,
    last_message_at: null,
    created_at: "2026-01-01T00:00:00Z",
    peer: { id: "U2", username: "bob" },
    unread_count: 0,
    ...overrides,
  };
}

describe("NewDMModal", () => {
  it("fetches /api/users on open and lists everyone except self, sorted by username", async () => {
    httpRequestMock.mockResolvedValueOnce({
      users: [
        { id: "U2", username: "carol" },
        { id: "U1", username: "alice" }, // self — must be filtered
        { id: "U3", username: "bob" },
      ],
    });
    render(<NewDMModal open={true} onClose={vi.fn()} selfUserId="U1" onCreate={vi.fn()} />);
    await waitFor(() => {
      expect(httpRequestMock).toHaveBeenCalledWith("GET", "/api/users");
    });
    const items = await screen.findAllByRole("listitem");
    expect(items.map((li) => li.textContent)).toEqual(["bob", "carol"]);
  });

  it("clicking a user calls onCreate(peerId), fires onCreated, and closes", async () => {
    httpRequestMock.mockResolvedValueOnce({
      users: [{ id: "U2", username: "bob" }],
    });
    const created = makeConv({ id: "DM1", peer: { id: "U2", username: "bob" } });
    const onCreate = vi.fn().mockResolvedValue(created);
    const onClose = vi.fn();
    const onCreated = vi.fn();
    render(
      <NewDMModal
        open={true}
        onClose={onClose}
        selfUserId="U1"
        onCreate={onCreate}
        onCreated={onCreated}
      />,
    );
    await screen.findByRole("button", { name: /direct message bob/i });
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /direct message bob/i }));
    await waitFor(() => {
      expect(onCreate).toHaveBeenCalledWith("U2");
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onCreated).toHaveBeenCalledWith(created);
  });

  it("renders an error when /api/users fails", async () => {
    httpRequestMock.mockRejectedValueOnce(
      new ApiError(503, "service_unavailable", "directory unavailable"),
    );
    render(<NewDMModal open={true} onClose={vi.fn()} selfUserId="U1" onCreate={vi.fn()} />);
    await screen.findByText(/directory unavailable/i);
  });

  it("renders a create-side error inline and keeps the modal open", async () => {
    httpRequestMock.mockResolvedValueOnce({
      users: [{ id: "U2", username: "bob" }],
    });
    const onCreate = vi.fn().mockRejectedValueOnce(new ApiError(429, "rate_limited", "Slow down."));
    const onClose = vi.fn();
    render(<NewDMModal open={true} onClose={onClose} selfUserId="U1" onCreate={onCreate} />);
    await screen.findByRole("button", { name: /direct message bob/i });
    const u = userEvent.setup();
    await u.click(screen.getByRole("button", { name: /direct message bob/i }));
    await screen.findByText(/slow down\./i);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("shows an empty-state when no other users exist", async () => {
    httpRequestMock.mockResolvedValueOnce({
      users: [{ id: "U1", username: "alice" }], // only self
    });
    render(<NewDMModal open={true} onClose={vi.fn()} selfUserId="U1" onCreate={vi.fn()} />);
    await screen.findByText(/no other users to message yet/i);
  });
});
