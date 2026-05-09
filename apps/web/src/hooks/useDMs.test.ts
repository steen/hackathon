import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { ApiError, type Conversation, type DMEvent, type ReadEvent } from "@hackathon/api-client";

const httpRequestMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: {
      request: httpRequestMock,
    },
  }),
}));

import { useDMs } from "./useDMs.js";
import type { ChatSocket, ChatSocketEventName, ChatSocketListener } from "./useChatSocket.js";
import { _resetAppErrorSinkForTests } from "../lib/userFacingError.js";

interface FakeSocket extends ChatSocket {
  emitDM: (ev: DMEvent) => void;
  emitRead: (ev: ReadEvent) => void;
  emitOpen: () => void;
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
    emitDM: (ev) => {
      for (const fn of listeners.dm) fn(ev);
    },
    emitRead: (ev) => {
      for (const fn of listeners.read) fn(ev);
    },
    emitOpen: () => {
      for (const fn of listeners.open) fn(undefined);
    },
  };
}

function makeConv(overrides: Partial<Conversation> = {}): Conversation {
  return {
    id: "DM1",
    user_a_id: "U1",
    user_b_id: "U2",
    last_message_id: "M1",
    last_message_at: "2026-01-02T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    peer: { id: "U2", username: "bob" },
    unread_count: 0,
    ...overrides,
  };
}

let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
  _resetAppErrorSinkForTests();
});

afterEach(() => {
  cleanup();
  httpRequestMock.mockReset();
  consoleErrorSpy.mockRestore();
  _resetAppErrorSinkForTests();
});

describe("useDMs", () => {
  it("seeds the list from GET /api/dms, sorted by last_message_at DESC", async () => {
    const older = makeConv({
      id: "DM-old",
      last_message_at: "2026-01-01T00:00:00Z",
      peer: { id: "U2", username: "bob" },
    });
    const newer = makeConv({
      id: "DM-new",
      last_message_at: "2026-01-03T00:00:00Z",
      peer: { id: "U3", username: "carol" },
    });
    httpRequestMock.mockResolvedValueOnce({ conversations: [older, newer] });
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1" }));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.conversations.map((c) => c.id)).toEqual(["DM-new", "DM-old"]);
  });

  it("surfaces a curated banner when listDMs fails", async () => {
    httpRequestMock.mockRejectedValueOnce(
      new ApiError(503, "service_unavailable", "internal-trace"),
    );
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1" }));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.error).toContain("Failed to load direct messages");
    expect(result.current.error).not.toContain("internal-trace");
  });

  it("dm frame for a peer message increments local unread_count above the server baseline", async () => {
    httpRequestMock.mockResolvedValueOnce({
      conversations: [makeConv({ unread_count: 2 })],
    });
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1", socket: sock }));
    await waitFor(() => {
      expect(result.current.conversations).toHaveLength(1);
    });
    act(() => {
      sock.emitDM({
        type: "dm",
        data: {
          conversation: makeConv({ unread_count: 2 }),
          dm_message: {
            id: "M2",
            conversation_id: "DM1",
            sender_user_id: "U2",
            body: "hi",
            created_at: "2026-01-02T00:01:00Z",
          },
        },
      });
    });
    // Local-incremented to 3 (server baseline 2 + 1).
    expect(result.current.conversations[0]?.unread_count).toBe(3);
  });

  it("dm frame for a self-sent message does NOT increment unread_count", async () => {
    httpRequestMock.mockResolvedValueOnce({
      conversations: [makeConv({ unread_count: 0 })],
    });
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1", socket: sock }));
    await waitFor(() => {
      expect(result.current.conversations).toHaveLength(1);
    });
    act(() => {
      sock.emitDM({
        type: "dm",
        data: {
          conversation: makeConv({ unread_count: 0 }),
          dm_message: {
            id: "M-self",
            conversation_id: "DM1",
            sender_user_id: "U1",
            body: "hi",
            created_at: "2026-01-02T00:01:00Z",
          },
        },
      });
    });
    expect(result.current.conversations[0]?.unread_count).toBe(0);
  });

  it("dm frame for an unknown conversation seeds the sidebar entry from the embedded block (§8)", async () => {
    httpRequestMock.mockResolvedValueOnce({ conversations: [] });
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1", socket: sock }));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    const fresh = makeConv({
      id: "DM-fresh",
      peer: { id: "U7", username: "dana" },
      unread_count: 1,
      last_message_at: "2026-01-04T00:00:00Z",
    });
    act(() => {
      sock.emitDM({
        type: "dm",
        data: {
          conversation: fresh,
          dm_message: {
            id: "M-fresh",
            conversation_id: "DM-fresh",
            sender_user_id: "U7",
            body: "first hello",
            created_at: "2026-01-04T00:00:00Z",
          },
        },
      });
    });
    expect(result.current.conversations).toHaveLength(1);
    expect(result.current.conversations[0]?.id).toBe("DM-fresh");
    expect(result.current.conversations[0]?.unread_count).toBe(1);
  });

  it("read frame with scope=dm zeroes the matching conversation's unread", async () => {
    httpRequestMock.mockResolvedValueOnce({
      conversations: [makeConv({ unread_count: 4 })],
    });
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1", socket: sock }));
    await waitFor(() => {
      expect(result.current.conversations).toHaveLength(1);
    });
    act(() => {
      sock.emitRead({
        type: "read",
        data: {
          scope: "dm",
          target_id: "DM1",
          last_read_message_id: "M9",
          unread_count: 0,
        },
      });
    });
    expect(result.current.conversations[0]?.unread_count).toBe(0);
  });

  it("read frame with scope=channel is ignored", async () => {
    httpRequestMock.mockResolvedValueOnce({
      conversations: [makeConv({ unread_count: 5 })],
    });
    const sock = makeFakeSocket();
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1", socket: sock }));
    await waitFor(() => {
      expect(result.current.conversations).toHaveLength(1);
    });
    act(() => {
      sock.emitRead({
        type: "read",
        data: {
          scope: "channel",
          target_id: "DM1",
          last_read_message_id: "M9",
          unread_count: 0,
        },
      });
    });
    expect(result.current.conversations[0]?.unread_count).toBe(5);
  });

  it("WS open triggers a reload (gap recovery → §12 reconcile-overwrite)", async () => {
    httpRequestMock.mockResolvedValueOnce({
      conversations: [makeConv({ unread_count: 3 })],
    });
    const sock = makeFakeSocket();
    renderHook(() => useDMs(true, { selfUserId: "U1", socket: sock }));
    await waitFor(() => {
      expect(httpRequestMock).toHaveBeenCalledTimes(1);
    });

    httpRequestMock.mockResolvedValueOnce({
      conversations: [makeConv({ unread_count: 0 })],
    });
    await act(async () => {
      sock.emitOpen();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(httpRequestMock).toHaveBeenCalledTimes(2);
    });
  });

  it("startWith() POSTs /api/dms and merges/updates the conversation", async () => {
    httpRequestMock.mockResolvedValueOnce({ conversations: [] });
    const { result } = renderHook(() => useDMs(true, { selfUserId: "U1" }));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    const fresh = makeConv({
      id: "DM-new",
      last_message_at: null,
      last_message_id: null,
      peer: { id: "U9", username: "eli" },
    });
    httpRequestMock.mockResolvedValueOnce(fresh);
    let returned: Conversation | undefined;
    await act(async () => {
      returned = await result.current.startWith("U9");
    });
    expect(httpRequestMock).toHaveBeenLastCalledWith("POST", "/api/dms", { peer_user_id: "U9" });
    expect(returned?.id).toBe("DM-new");
    expect(result.current.conversations.map((c) => c.id)).toEqual(["DM-new"]);
  });
});
