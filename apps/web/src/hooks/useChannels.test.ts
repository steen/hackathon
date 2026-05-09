import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { ApiError, type Event as WsEvent, type ReadEvent } from "@hackathon/api-client";

const listChannelsMock = vi.fn();
const createChannelMock = vi.fn();
const renameChannelMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    listChannels: listChannelsMock,
    createChannel: createChannelMock,
    renameChannel: renameChannelMock,
  }),
}));

import { useChannels } from "./useChannels.js";
import type { ChatSocket, ChatSocketEventName, ChatSocketListener } from "./useChatSocket.js";
import { _resetAppErrorSinkForTests, useAppError } from "../lib/userFacingError.js";

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

interface FakeSocket extends ChatSocket {
  emitMessage: (ev: WsEvent) => void;
  emitOpen: () => void;
  emitRead: (ev: ReadEvent) => void;
}

function makeFakeSocket(): FakeSocket {
  const listeners = {
    open: new Set<ChatSocketListener<"open">>(),
    close: new Set<ChatSocketListener<"close">>(),
    error: new Set<ChatSocketListener<"error">>(),
    message: new Set<ChatSocketListener<"message">>(),
    dm: new Set<ChatSocketListener<"dm">>(),
    read: new Set<ChatSocketListener<"read">>(),
  };
  return {
    subscribe: <E extends ChatSocketEventName>(event: E, fn: ChatSocketListener<E>) => {
      const set = listeners[event] as Set<ChatSocketListener<E>>;
      set.add(fn);
      return () => {
        set.delete(fn);
      };
    },
    emitOpen: () => {
      for (const fn of listeners.open) fn(undefined);
    },
    emitMessage: (ev) => {
      for (const fn of listeners.message) fn(ev);
    },
    emitRead: (ev) => {
      for (const fn of listeners.read) fn(ev);
    },
  };
}

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  _resetAppErrorSinkForTests();
});

afterEach(() => {
  cleanup();
  listChannelsMock.mockReset();
  createChannelMock.mockReset();
  renameChannelMock.mockReset();
  consoleErrorSpy.mockRestore();
  _resetAppErrorSinkForTests();
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

  it("dispatches the curated error into the shared app-error sink", async () => {
    const raw = new ApiError(503, "service_unavailable", "internal-trace-99");
    listChannelsMock.mockRejectedValueOnce(raw);
    const { result: hook } = renderHook(() => useChannels(true));
    await waitFor(() => {
      expect(hook.current.loading).toBe(false);
    });
    const { result: sink } = renderHook(() => useAppError());
    await waitFor(() => {
      expect(sink.current).toBe(
        "Failed to load channels: The server is having trouble right now. Please try again.",
      );
    });
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

  it("create() POSTs and merges the new channel without a redundant reload()", async () => {
    listChannelsMock.mockResolvedValueOnce([{ id: "C1", name: "general" }]);
    const created = { id: "C2", name: "books", created_at: "2026-01-01T00:00:00Z" };
    createChannelMock.mockResolvedValueOnce(created);
    const { result } = renderHook(() => useChannels(true));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(1);
    });
    let returned: { id: string; name: string } | undefined;
    await act(async () => {
      returned = await result.current.create("books", { isPublic: false });
    });
    expect(createChannelMock).toHaveBeenCalledWith("books", { isPublic: false });
    expect(returned?.id).toBe("C2");
    expect(result.current.channels.map((c) => c.id)).toEqual(["C1", "C2"]);
    // Only the mount-effect reload — no extra refetch on create.
    expect(listChannelsMock).toHaveBeenCalledTimes(1);
  });

  it("rename() PATCHes and updates the channel name in place", async () => {
    listChannelsMock.mockResolvedValueOnce([
      { id: "C1", name: "general" },
      { id: "C2", name: "books" },
    ]);
    const renamed = { id: "C2", name: "reading", created_at: "2026-01-01T00:00:00Z" };
    renameChannelMock.mockResolvedValueOnce(renamed);
    const { result } = renderHook(() => useChannels(true));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(2);
    });
    await act(async () => {
      await result.current.rename("C2", "reading");
    });
    expect(renameChannelMock).toHaveBeenCalledWith("C2", "reading");
    expect(result.current.channels.map((c) => c.name)).toEqual(["general", "reading"]);
    expect(result.current.channels.map((c) => c.id)).toEqual(["C1", "C2"]);
  });

  it("WS channel:create event upserts; duplicate event is a no-op", async () => {
    listChannelsMock.mockResolvedValueOnce([{ id: "C1", name: "general" }]);
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(1);
    });

    const created = { id: "C2", name: "books", created_at: "2026-01-01T00:00:00Z" };
    act(() => {
      sock.emitMessage({ type: "channel", data: { kind: "create", channel: created } });
    });
    expect(result.current.channels.map((c) => c.id)).toEqual(["C1", "C2"]);

    // Duplicate frame — same id — must not append a second row.
    act(() => {
      sock.emitMessage({ type: "channel", data: { kind: "create", channel: created } });
    });
    expect(result.current.channels.map((c) => c.id)).toEqual(["C1", "C2"]);
  });

  it("WS channel:rename event updates name in place", async () => {
    listChannelsMock.mockResolvedValueOnce([
      { id: "C1", name: "general" },
      { id: "C2", name: "books" },
    ]);
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(2);
    });
    act(() => {
      sock.emitMessage({
        type: "channel",
        data: {
          kind: "rename",
          channel: { id: "C2", name: "reading", created_at: "2026-01-01T00:00:00Z" },
        },
      });
    });
    expect(result.current.channels.map((c) => c.name)).toEqual(["general", "reading"]);
    expect(result.current.channels.map((c) => c.id)).toEqual(["C1", "C2"]);
  });

  it("ignores non-channel WS frames", async () => {
    listChannelsMock.mockResolvedValueOnce([{ id: "C1", name: "general" }]);
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(1);
    });
    act(() => {
      sock.emitMessage({
        type: "message",
        data: {
          id: "M1",
          channel_id: "C1",
          sender_user_id: "U1",
          body: "hi",
          created_at: "2026-01-01T00:00:00Z",
        },
      });
    });
    expect(result.current.channels).toHaveLength(1);
  });

  it("WS read:channel frame zeroes unread_count on the matching channel", async () => {
    listChannelsMock.mockResolvedValueOnce([
      {
        id: "C1",
        name: "general",
        created_at: "2026-01-01T00:00:00Z",
        unread_count: 5,
        last_read_message_id: "M0",
      },
      {
        id: "C2",
        name: "books",
        created_at: "2026-01-01T00:00:00Z",
        unread_count: 2,
        last_read_message_id: null,
      },
    ]);
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(2);
    });
    expect(result.current.channels[0]?.unread_count).toBe(5);
    expect(result.current.channels[1]?.unread_count).toBe(2);

    act(() => {
      sock.emitRead({
        type: "read",
        data: {
          scope: "channel",
          target_id: "C1",
          last_read_message_id: "M9",
          unread_count: 0,
        },
      });
    });
    expect(result.current.channels[0]?.unread_count).toBe(0);
    expect(result.current.channels[0]?.last_read_message_id).toBe("M9");
    // Other channels untouched.
    expect(result.current.channels[1]?.unread_count).toBe(2);
  });

  it("WS read:channel frame for an unknown id is a no-op", async () => {
    listChannelsMock.mockResolvedValueOnce([
      {
        id: "C1",
        name: "general",
        created_at: "2026-01-01T00:00:00Z",
        unread_count: 3,
      },
    ]);
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(1);
    });
    const before = result.current.channels;
    act(() => {
      sock.emitRead({
        type: "read",
        data: {
          scope: "channel",
          target_id: "C-UNKNOWN",
          last_read_message_id: "M9",
          unread_count: 0,
        },
      });
    });
    expect(result.current.channels).toBe(before);
  });

  it("WS read:dm frame is ignored by useChannels", async () => {
    listChannelsMock.mockResolvedValueOnce([
      {
        id: "C1",
        name: "general",
        created_at: "2026-01-01T00:00:00Z",
        unread_count: 4,
      },
    ]);
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      expect(result.current.channels).toHaveLength(1);
    });
    act(() => {
      sock.emitRead({
        type: "read",
        data: {
          scope: "dm",
          target_id: "C1",
          last_read_message_id: "M9",
          unread_count: 0,
        },
      });
    });
    expect(result.current.channels[0]?.unread_count).toBe(4);
  });

  it("calls reload() on every WS open (initial + reconnect) for catchup", async () => {
    listChannelsMock.mockResolvedValue([{ id: "C1", name: "general" }]);
    const sock = makeFakeSocket();
    renderHook(() => useChannels(true, { socket: sock }));
    await waitFor(() => {
      // mount-effect reload is the first call.
      expect(listChannelsMock).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      sock.emitOpen();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(listChannelsMock).toHaveBeenCalledTimes(2);
    });

    // Reconnect: another open event.
    await act(async () => {
      sock.emitOpen();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(listChannelsMock).toHaveBeenCalledTimes(3);
    });
  });
});
