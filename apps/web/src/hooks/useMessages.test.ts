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

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: {
      wsTicket: wsTicketMock,
      getBaseUrl: () => "http://test.local",
    },
    listMessages: listMessagesMock,
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
});

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
