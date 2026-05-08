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
  emit(payload: unknown): void {
    this.onmessage?.({ data: JSON.stringify(payload) });
  }
}

const wsTicketMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: {
      wsTicket: wsTicketMock,
      getBaseUrl: () => "http://test.local",
    },
  }),
}));

import { useChatSocket } from "./useChatSocket.js";
import { _resetAppErrorSinkForTests } from "../lib/userFacingError.js";

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
  _resetAppErrorSinkForTests();
});

describe("useChatSocket", () => {
  it("starts idle when channelId is null", () => {
    const { result } = renderHook(() => useChatSocket(null));
    expect(result.current.connection).toBe("idle");
    expect(result.current.error).toBeNull();
    expect(FakeSocket.instances).toHaveLength(0);
  });

  it("opens a socket when channelId becomes non-null and reports connecting → open", async () => {
    const { result } = renderHook(() => useChatSocket("C1"));
    expect(result.current.connection).toBe("connecting");
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
  });

  it("delivers message frames to subscribers", async () => {
    const { result } = renderHook(() => useChatSocket("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const received: { type: string; data: unknown }[] = [];
    let unsubscribe: (() => void) | undefined;
    act(() => {
      unsubscribe = result.current.socket.subscribe("message", (ev) => {
        received.push(ev);
      });
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    await act(async () => {
      FakeSocket.instances[0]?.emit({ type: "message", data: { id: "M1" } });
      await Promise.resolve();
    });
    expect(received).toEqual([{ type: "message", data: { id: "M1" } }]);
    unsubscribe?.();
  });

  it("flips to reconnecting on a dropped connection and back to open on the new socket", async () => {
    const { result } = renderHook(() => useChatSocket("C1"));
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
    await waitFor(() => {
      expect(result.current.connection).toBe("reconnecting");
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
      expect(result.current.connection).toBe("open");
    });
  });

  it("fires open subscribers on every (re)open, with an openCount the consumer can use", async () => {
    const { result } = renderHook(() => useChatSocket("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    let opens = 0;
    act(() => {
      result.current.socket.subscribe("open", () => {
        opens += 1;
      });
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    expect(opens).toBe(1);

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
    expect(opens).toBe(2);
  });

  it("close subscribers receive the close event, error subscribers stay separate", async () => {
    const { result } = renderHook(() => useChatSocket("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const closes: { code: number; reason: string }[] = [];
    const errors: unknown[] = [];
    act(() => {
      result.current.socket.subscribe("close", (ev) => {
        closes.push(ev);
      });
      result.current.socket.subscribe("error", (ev) => {
        errors.push(ev);
      });
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      await Promise.resolve();
    });
    await act(async () => {
      FakeSocket.instances[0]?.forceClose();
      await Promise.resolve();
    });
    expect(closes).toHaveLength(1);
    expect(closes[0]?.code).toBe(1006);
    expect(errors).toHaveLength(0);
  });

  it("unsubscribe stops a listener from receiving further events", async () => {
    const { result } = renderHook(() => useChatSocket("C1"));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    let count = 0;
    let off: (() => void) | undefined;
    act(() => {
      off = result.current.socket.subscribe("message", () => {
        count += 1;
      });
    });
    await act(async () => {
      FakeSocket.instances[0]?.open();
      FakeSocket.instances[0]?.emit({ type: "message", data: { id: "M1" } });
      await Promise.resolve();
    });
    expect(count).toBe(1);
    act(() => {
      off?.();
    });
    await act(async () => {
      FakeSocket.instances[0]?.emit({ type: "message", data: { id: "M2" } });
      await Promise.resolve();
    });
    expect(count).toBe(1);
  });

  it("closes the socket and collapses to idle when channelId flips back to null", async () => {
    const { result, rerender } = renderHook(({ ch }: { ch: string | null }) => useChatSocket(ch), {
      initialProps: { ch: "C1" },
    });
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
    act(() => {
      rerender({ ch: null });
    });
    await waitFor(() => {
      expect(result.current.connection).toBe("idle");
    });
    expect(FakeSocket.instances[0]?.readyState).toBe(3);
  });

  it("opens a fresh socket when channelId changes", async () => {
    const { rerender } = renderHook(({ ch }: { ch: string }) => useChatSocket(ch), {
      initialProps: { ch: "C1" },
    });
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    expect(FakeSocket.instances[0]?.url).toContain("channel=C1");
    act(() => {
      rerender({ ch: "C2" });
    });
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(2);
    });
    expect(FakeSocket.instances[1]?.url).toContain("channel=C2");
    // Previous socket got closed by the lifecycle teardown.
    expect(FakeSocket.instances[0]?.readyState).toBe(3);
  });

  it("surfaces a curated error when the ticket fetch rejects", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    wsTicketMock.mockReset();
    wsTicketMock.mockRejectedValueOnce(new Error("ticket boom"));

    const { result } = renderHook(() => useChatSocket("C1"));
    await waitFor(() => {
      expect(result.current.error).not.toBeNull();
    });
    expect(result.current.connection).toBe("reconnecting");
    expect(result.current.error).toBe("Message connection failed: Something went wrong.");
    expect(result.current.error ?? "").not.toContain("ticket boom");
    consoleErrorSpy.mockRestore();
  });
});
