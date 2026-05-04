import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { ApiError } from "@hackathon/api-client";

const listChannelsMock = vi.fn();
const createChannelMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    listChannels: listChannelsMock,
    createChannel: createChannelMock,
  }),
}));

import { useChannels } from "./useChannels.js";

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  listChannelsMock.mockReset();
  createChannelMock.mockReset();
  consoleErrorSpy.mockRestore();
});

describe("useChannels", () => {
  it("seeds the list from listChannels()", async () => {
    listChannelsMock.mockResolvedValue([
      { id: "C1", name: "general" },
      { id: "C2", name: "random" },
    ]);
    const { result } = renderHook(() => useChannels(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.channels.map((c) => c.id)).toEqual(["C1", "C2"]);
    expect(result.current.error).toBeNull();
  });

  it("surfaces a curated error when listChannels fails without echoing raw err.message", async () => {
    const raw = new ApiError(503, "service_unavailable", "internal-db-trace-77");
    listChannelsMock.mockRejectedValueOnce(raw);
    const { result } = renderHook(() => useChannels(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.error).toBe(
      "Failed to load channels: The server is having trouble right now. Please try again.",
    );
    expect(result.current.error).not.toContain("internal-db-trace-77");
    expect(result.current.error).not.toContain("503");
    expect(consoleErrorSpy).toHaveBeenCalledWith("Failed to load channels", raw);
  });

  it("maps a fetch TypeError to the curated network copy", async () => {
    const raw = new TypeError("Failed to fetch xyz-internal-detail");
    listChannelsMock.mockRejectedValueOnce(raw);
    const { result } = renderHook(() => useChannels(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.error).toBe(
      "Failed to load channels: Could not reach the server. Check your connection and try again.",
    );
    expect(result.current.error).not.toContain("xyz-internal-detail");
    expect(result.current.error).not.toContain("Failed to fetch");
  });
});
