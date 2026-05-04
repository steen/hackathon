import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";

class FakeSocket {
  static instances: FakeSocket[] = [];
  url: string;
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onclose: ((ev: { code: number; reason: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeSocket.instances.push(this);
  }
  open(): void {
    this.readyState = 1;
    this.onopen?.(undefined);
  }
  send(_data: string): void {
    /* noop */
  }
  close(): void {
    this.readyState = 3;
    this.onclose?.({ code: 1000, reason: "normal" });
  }
  forceClose(): void {
    this.readyState = 3;
    this.onclose?.({ code: 1006, reason: "abnormal" });
  }
}

const wsTicketMock = vi.fn();
const listMessagesMock = vi.fn();
const postMessageMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: {
      wsTicket: wsTicketMock,
      getBaseUrl: () => "http://test.local",
    },
    listMessages: listMessagesMock,
    postMessage: postMessageMock,
  }),
}));

import { useMessages } from "./useMessages.js";

interface MsgRow {
  id: string;
  channel_id: string;
  sender_user_id: string;
  body: string;
  created_at: string;
}

function msg(id: string, body: string): MsgRow {
  return {
    id,
    channel_id: "C1",
    sender_user_id: "U1",
    body,
    created_at: "2026-01-01T00:00:00Z",
  };
}

beforeEach(() => {
  (globalThis as { WebSocket?: unknown }).WebSocket = FakeSocket;
  wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
});

afterEach(() => {
  cleanup();
  FakeSocket.instances = [];
  delete (globalThis as { WebSocket?: unknown }).WebSocket;
  wsTicketMock.mockReset();
  listMessagesMock.mockReset();
  postMessageMock.mockReset();
});

function userMsg(id: string, body: string, createdAt: string): MsgRow {
  return {
    id,
    channel_id: "C1",
    sender_user_id: "U1",
    body,
    created_at: createdAt,
  };
}

describe("useMessages", () => {
  it("seeds from listMessages and appends live WS frames", async () => {
    listMessagesMock.mockResolvedValueOnce([msg("M1", "hello")]);
    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1"]);
    });
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    await act(async () => {
      sock?.open();
      await Promise.resolve();
    });
    await act(async () => {
      sock?.onmessage?.({
        data: JSON.stringify({ type: "message", data: msg("M2", "world") }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2"]);
    });
  });

  it("reverses initial history (server newest-first → state oldest-first)", async () => {
    // Server contract: listMessages returns newest-first to match the
    // `before` cursor. The hook must flip that at the boundary so the
    // rendered list reads oldest→newest with the composer under the newest.
    listMessagesMock.mockResolvedValueOnce([
      msg("M3", "third"),
      msg("M2", "second"),
      msg("M1", "first"),
    ]);
    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2", "M3"]);
    });
  });

  it("appends a live WS frame below the most recent history entry", async () => {
    listMessagesMock.mockResolvedValueOnce([
      msg("M3", "third"),
      msg("M2", "second"),
      msg("M1", "first"),
    ]);
    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2", "M3"]);
    });
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    await act(async () => {
      sock?.open();
      await Promise.resolve();
    });
    await act(async () => {
      sock?.onmessage?.({
        data: JSON.stringify({ type: "message", data: msg("M4", "live") }),
      });
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2", "M3", "M4"]);
    });
  });

  it("optimistic pending entry sits at the bottom, below older history", async () => {
    listMessagesMock.mockResolvedValueOnce([
      msg("M3", "third"),
      msg("M2", "second"),
      msg("M1", "first"),
    ]);
    postMessageMock.mockImplementation(async () => {
      await Promise.resolve();
      return userMsg("M-server", "draft", "2026-01-01T00:00:01Z");
    });

    const { result } = renderHook(() => useMessages("C1", "U1"));
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2", "M3"]);
    });
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });

    await act(async () => {
      await result.current.send("draft");
    });

    const ids = result.current.messages.map((m) => m.id);
    expect(ids.slice(0, 3)).toEqual(["M1", "M2", "M3"]);
    expect(ids).toHaveLength(4);
    const last = result.current.messages[3];
    expect(last?.id.startsWith("pending-")).toBe(true);
    expect(last?.status).toBe("pending");
    expect(last?.body).toBe("draft");
  });

  it("does NOT refetch on the initial WS open", async () => {
    listMessagesMock.mockResolvedValueOnce([msg("M1", "hello")]);
    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    await act(async () => {
      sock?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.connection).toBe("open");
    });
    // Only the one initial history fetch — no second fetch on open.
    expect(listMessagesMock).toHaveBeenCalledTimes(1);
  });

  it("on reopen after a forced close, refetches and merges missed messages", async () => {
    // 1) Initial history.
    listMessagesMock.mockResolvedValueOnce([msg("M1", "before-outage")]);
    // 2) Catchup on reopen returns the during-outage message plus the
    //    one we already have (server returns a window). Hook must dedup
    //    by id so M1 is not duplicated.
    listMessagesMock.mockResolvedValueOnce([
      msg("M2", "during-outage"),
      msg("M1", "before-outage"),
    ]);

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock1 = FakeSocket.instances[0];
    await act(async () => {
      sock1?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.connection).toBe("open");
    });
    expect(result.current.messages.map((m) => m.id)).toEqual(["M1"]);
    expect(listMessagesMock).toHaveBeenCalledTimes(1);

    // Drop the socket. The api-client schedules a reconnect via setTimeout;
    // jsdom uses real timers, so we wait for the second instance to appear.
    await act(async () => {
      sock1?.forceClose();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.connection).toBe("reconnecting");
    });

    await waitFor(
      () => {
        expect(FakeSocket.instances).toHaveLength(2);
      },
      { timeout: 2000 },
    );
    const sock2 = FakeSocket.instances[1];
    await act(async () => {
      sock2?.open();
      await Promise.resolve();
    });

    // Reopen triggers the catchup fetch. Wait for it to resolve and
    // M2 to land in state.
    await waitFor(() => {
      expect(listMessagesMock).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2"]);
    });
    // The catchup call uses limit=50 (no before cursor — server has no
    // `after=` param; dedup by id covers the overlap).
    expect(listMessagesMock).toHaveBeenLastCalledWith("C1", { limit: 50 });
  });

  it("multi-message catchup lands in chronological order, not server (newest-first) order", async () => {
    // Initial history.
    listMessagesMock.mockResolvedValueOnce([msg("M1", "before-outage")]);
    // Catchup mimics server: newest-first window. Hook must reverse the
    // fresh additions so they read chronologically when appended.
    listMessagesMock.mockResolvedValueOnce([
      msg("M5", "during-outage-5"),
      msg("M4", "during-outage-4"),
      msg("M3", "during-outage-3"),
      msg("M2", "during-outage-2"),
      msg("M1", "before-outage"),
    ]);

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.connection).toBe("open");
    });

    await act(async () => {
      FakeSocket.instances[0]?.forceClose();
      await Promise.resolve();
    });
    await waitFor(
      () => {
        expect(FakeSocket.instances).toHaveLength(2);
      },
      { timeout: 2000 },
    );
    await act(async () => {
      FakeSocket.instances[1]?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(listMessagesMock).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1", "M2", "M3", "M4", "M5"]);
    });
  });

  it("send appends an optimistic pending entry immediately, then reconciles when the WS frame arrives", async () => {
    listMessagesMock.mockResolvedValueOnce([]);
    postMessageMock.mockImplementation(async () => {
      await Promise.resolve();
      return userMsg("M-server", "hi there", "2026-01-01T00:00:00.500Z");
    });

    const { result } = renderHook(() => useMessages("C1", "U1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.connection).toBe("open");
    });

    await act(async () => {
      await result.current.send("hi there");
    });

    // Immediately after send, the optimistic entry is present with a
    // pending- prefix and status "pending".
    expect(result.current.messages).toHaveLength(1);
    const pending = result.current.messages[0];
    expect(pending?.id.startsWith("pending-")).toBe(true);
    expect(pending?.status).toBe("pending");
    expect(pending?.body).toBe("hi there");
    expect(postMessageMock).toHaveBeenCalledWith("C1", "hi there");

    // Server's WS frame echoes the persisted message — same body, same
    // sender, recent created_at — and the hook swaps the pending entry
    // for the server row in place (no double-render).
    await act(async () => {
      FakeSocket.instances[0]?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: userMsg("M-server", "hi there", "2026-01-01T00:00:00.500Z"),
        }),
      });
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M-server"]);
    });
    expect(result.current.messages[0]?.status).toBeUndefined();
  });

  it("send marks the optimistic entry failed when REST POST rejects", async () => {
    listMessagesMock.mockResolvedValueOnce([]);
    postMessageMock.mockRejectedValueOnce(new Error("network down"));

    const { result } = renderHook(() => useMessages("C1", "U1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });

    await act(async () => {
      await result.current.send("doomed");
    });

    expect(result.current.messages).toHaveLength(1);
    const failed = result.current.messages[0];
    expect(failed?.id.startsWith("pending-")).toBe(true);
    expect(failed?.status).toBe("failed");
    expect(failed?.body).toBe("doomed");
    // Per-entry status carries the failure; channel-level `error` stays
    // reserved for history/socket faults.
    expect(result.current.error).toBeNull();
  });

  it("optimistic entry is de-duped against the WS frame, not appended twice", async () => {
    listMessagesMock.mockResolvedValueOnce([]);
    postMessageMock.mockImplementation(async () => {
      await Promise.resolve();
      return userMsg("M-srv", "echo", "2026-01-01T00:00:00.250Z");
    });

    const { result } = renderHook(() => useMessages("C1", "U1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });

    await act(async () => {
      await result.current.send("echo");
    });
    expect(result.current.messages).toHaveLength(1);

    await act(async () => {
      FakeSocket.instances[0]?.onmessage?.({
        data: JSON.stringify({
          type: "message",
          data: userMsg("M-srv", "echo", "2026-01-01T00:00:00.250Z"),
        }),
      });
      await Promise.resolve();
    });

    // Final state has exactly one row — the persisted server message —
    // not the pending entry plus the live frame.
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M-srv"]);
    });
    expect(result.current.messages).toHaveLength(1);
  });

  it("a failed catchup leaves the existing list intact", async () => {
    listMessagesMock.mockResolvedValueOnce([msg("M1", "hello")]);
    listMessagesMock.mockRejectedValueOnce(new Error("network down"));

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(result.current.messages.map((m) => m.id)).toEqual(["M1"]);
    });

    await act(async () => {
      FakeSocket.instances[0]?.forceClose();
      await Promise.resolve();
    });
    await waitFor(
      () => {
        expect(FakeSocket.instances).toHaveLength(2);
      },
      { timeout: 2000 },
    );
    await act(async () => {
      FakeSocket.instances[1]?.open();
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(listMessagesMock).toHaveBeenCalledTimes(2);
    });
    // List is unchanged; no error surfaced (catchup failure stays silent).
    expect(result.current.messages.map((m) => m.id)).toEqual(["M1"]);
    expect(result.current.error).toBeNull();
  });
});
