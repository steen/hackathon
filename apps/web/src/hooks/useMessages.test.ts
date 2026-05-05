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
import { _resetAppErrorSinkForTests, useAppError } from "../lib/userFacingError.js";

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
  _resetAppErrorSinkForTests();
});

afterEach(() => {
  cleanup();
  FakeSocket.instances = [];
  delete (globalThis as { WebSocket?: unknown }).WebSocket;
  wsTicketMock.mockReset();
  listMessagesMock.mockReset();
  postMessageMock.mockReset();
  _resetAppErrorSinkForTests();
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

  it("send plumbs a curated failureReason onto the failed entry", async () => {
    listMessagesMock.mockResolvedValueOnce([]);
    // A plain Error reaches REASON_GENERIC. Asserting the exact curated
    // string (not a substring of the raw err) is the contract: the raw
    // err.message must never leak into the row.
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const raw = new Error("boom internal-trace-77");
    postMessageMock.mockRejectedValueOnce(raw);

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

    const failed = result.current.messages[0];
    expect(failed?.status).toBe("failed");
    expect(failed?.failureReason).toBe("Something went wrong.");
    expect(failed?.failureReason ?? "").not.toContain("boom");
    expect(failed?.failureReason ?? "").not.toContain("internal-trace-77");
    expect(consoleErrorSpy).toHaveBeenCalledWith("Failed to send message", raw);
    consoleErrorSpy.mockRestore();
  });

  it("retry clears a stale failureReason on the row", async () => {
    listMessagesMock.mockResolvedValueOnce([]);
    postMessageMock.mockRejectedValueOnce(new Error("first"));
    // Second attempt hangs so the row sits in `pending` for the assertion.
    let resolveSecond: (() => void) | undefined;
    postMessageMock.mockImplementationOnce(
      () =>
        new Promise<void>((resolve) => {
          resolveSecond = () => {
            resolve();
          };
        }),
    );
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);

    const { result } = renderHook(() => useMessages("C1", "U1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    await act(async () => {
      await result.current.send("retry-me");
    });
    const failedId = result.current.messages[0]?.id ?? "";
    expect(result.current.messages[0]?.failureReason).toBe("Something went wrong.");

    await act(async () => {
      void result.current.retry(failedId);
      await Promise.resolve();
    });
    const pending = result.current.messages[0];
    expect(pending?.status).toBe("pending");
    expect(pending?.failureReason).toBeUndefined();

    resolveSecond?.();
    consoleErrorSpy.mockRestore();
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

  it("loadOlder prepends a reversed page above existing messages (50 + 50 = 100, oldest first)", async () => {
    // Initial page: 50 newest-first rows. With ULIDs that sort
    // lexicographically, M050 is the latest and M001 is the oldest of the
    // page. After reverse the state reads M001 -> M050.
    const initial = Array.from({ length: 50 }, (_, i) =>
      msg(`M${String(50 - i).padStart(3, "0")}`, `body-${String(50 - i)}`),
    );
    // Older page (in response to before=M001): 50 newest-first rows older
    // than M001. M-051 is the newest of the page (just before M001),
    // M-100 is the oldest.
    const older = Array.from({ length: 50 }, (_, i) =>
      msg(`M${String(100 - i).padStart(4, "0")}`, `older-${String(100 - i)}`),
    );
    listMessagesMock.mockResolvedValueOnce(initial);
    listMessagesMock.mockResolvedValueOnce(older);

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages).toHaveLength(50);
    });
    expect(result.current.canLoadOlder).toBe(true);
    // First (top-of-list) entry is the oldest of the initial page.
    expect(result.current.messages[0]?.id).toBe("M001");

    await act(async () => {
      await result.current.loadOlder();
    });

    expect(listMessagesMock).toHaveBeenLastCalledWith("C1", {
      before: "M001",
      limit: 50,
    });
    expect(result.current.messages).toHaveLength(100);
    // Older block sits at the top, oldest first; newest of the block sits
    // immediately above the previous-top row M001.
    const ids = result.current.messages.map((m) => m.id);
    expect(ids[0]).toBe("M0051");
    expect(ids[49]).toBe("M0100");
    expect(ids[50]).toBe("M001");
    expect(ids[99]).toBe("M050");
    expect(result.current.canLoadOlder).toBe(true);
  });

  it("loadOlder twice prepends two pages in order (50 + 50 + 50 = 150, oldest first)", async () => {
    const initial = Array.from({ length: 50 }, (_, i) =>
      msg(`B${String(50 - i).padStart(3, "0")}`, `b-${String(50 - i)}`),
    );
    // First older page: 50 rows older than B001, IDs A050..A001 in
    // newest-first server order; reversed in state to A001..A050.
    const olderA = Array.from({ length: 50 }, (_, i) =>
      msg(`A${String(50 - i).padStart(3, "0")}`, `a-${String(50 - i)}`),
    );
    // Second older page: 50 rows older than A001, IDs Z050..Z001.
    // (Pretend Z sorts before A for the purpose of this test — we only
    // check ordering of the prepended block, not lexicographic sanity of
    // the IDs themselves.)
    const olderZ = Array.from({ length: 50 }, (_, i) =>
      msg(`Z${String(50 - i).padStart(3, "0")}`, `z-${String(50 - i)}`),
    );
    listMessagesMock.mockResolvedValueOnce(initial);
    listMessagesMock.mockResolvedValueOnce(olderA);
    listMessagesMock.mockResolvedValueOnce(olderZ);

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages).toHaveLength(50);
    });

    await act(async () => {
      await result.current.loadOlder();
    });
    expect(result.current.messages).toHaveLength(100);
    expect(result.current.messages[0]?.id).toBe("A001");

    await act(async () => {
      await result.current.loadOlder();
    });

    expect(listMessagesMock).toHaveBeenLastCalledWith("C1", {
      before: "A001",
      limit: 50,
    });
    expect(result.current.messages).toHaveLength(150);
    const ids = result.current.messages.map((m) => m.id);
    // Top-of-list is now the oldest of the second prepended block.
    expect(ids[0]).toBe("Z001");
    expect(ids[49]).toBe("Z050");
    expect(ids[50]).toBe("A001");
    expect(ids[99]).toBe("A050");
    expect(ids[100]).toBe("B001");
    expect(ids[149]).toBe("B050");
  });

  it("loadOlder dedups rows already in state (server returns an overlapping row)", async () => {
    const initial = Array.from({ length: 50 }, (_, i) =>
      msg(`M${String(50 - i).padStart(3, "0")}`, `body-${String(50 - i)}`),
    );
    // Older page: 49 fresh rows + 1 row (M001) that is already in state.
    // The hook must skip the duplicate and prepend only 49 rows.
    const older: MsgRow[] = [
      ...Array.from({ length: 49 }, (_, i) =>
        msg(`M${String(99 - i).padStart(4, "0")}`, `older-${String(99 - i)}`),
      ),
      msg("M001", "body-1"),
    ];
    listMessagesMock.mockResolvedValueOnce(initial);
    listMessagesMock.mockResolvedValueOnce(older);

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages).toHaveLength(50);
    });

    await act(async () => {
      await result.current.loadOlder();
    });

    expect(result.current.messages).toHaveLength(99);
    const ids = result.current.messages.map((m) => m.id);
    // Reversed older page is M0051..M0099 then M001 (overlap). The dedup
    // strips the M001 duplicate, leaving 49 prepended rows above the
    // existing M001..M050.
    expect(ids[0]).toBe("M0051");
    expect(ids[48]).toBe("M0099");
    expect(ids[49]).toBe("M001");
    expect(ids[98]).toBe("M050");
    // Page came back full (50) so the "more might exist" heuristic stays on.
    expect(result.current.canLoadOlder).toBe(true);
  });

  it("canLoadOlder stays false when initial history is short", async () => {
    listMessagesMock.mockResolvedValueOnce([msg("M1", "only")]);
    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.messages).toHaveLength(1);
    });
    expect(result.current.canLoadOlder).toBe(false);
  });

  it("canLoadOlder flips off when an older page returns short", async () => {
    const initial = Array.from({ length: 50 }, (_, i) =>
      msg(`M${String(50 - i).padStart(3, "0")}`, `body-${String(50 - i)}`),
    );
    // Older page returns only 10 rows — fewer than the limit, so the
    // channel's start is now visible and the trigger should hide.
    const older = Array.from({ length: 10 }, (_, i) =>
      msg(`O${String(10 - i).padStart(3, "0")}`, `older-${String(10 - i)}`),
    );
    listMessagesMock.mockResolvedValueOnce(initial);
    listMessagesMock.mockResolvedValueOnce(older);

    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.canLoadOlder).toBe(true);
    });

    await act(async () => {
      await result.current.loadOlder();
    });

    expect(result.current.messages).toHaveLength(60);
    expect(result.current.canLoadOlder).toBe(false);
  });

  it("dispatches the curated history error into the shared app-error sink", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    listMessagesMock.mockRejectedValueOnce(new Error("history boom"));
    const { result: hook } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(hook.current.error).not.toBeNull();
    });
    const { result: sink } = renderHook(() => useAppError());
    await waitFor(() => {
      expect(sink.current).toBe("Failed to load message history: Something went wrong.");
    });
    consoleErrorSpy.mockRestore();
  });

  it("surfaces a curated error when initial history fails without echoing the raw err.message", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const raw = new Error("history boom internal-stack-trace-42");
    listMessagesMock.mockRejectedValueOnce(raw);
    const { result } = renderHook(() => useMessages("C1"));
    await waitFor(() => {
      expect(result.current.error).not.toBeNull();
    });
    expect(result.current.error).toBe("Failed to load message history: Something went wrong.");
    expect(result.current.error).not.toContain("history boom");
    expect(result.current.error).not.toContain("internal-stack-trace-42");
    expect(consoleErrorSpy).toHaveBeenCalledWith("Failed to load message history", raw);
    consoleErrorSpy.mockRestore();
  });
});
