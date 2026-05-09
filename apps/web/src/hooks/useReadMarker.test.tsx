import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";

const { markChannelReadMock, markDMReadMock } = vi.hoisted(() => ({
  markChannelReadMock: vi.fn(),
  markDMReadMock: vi.fn(),
}));

vi.mock("@hackathon/api-client", async () => {
  const actual = await vi.importActual<Record<string, unknown>>("@hackathon/api-client");
  return {
    ...actual,
    markChannelRead: markChannelReadMock,
    markDMRead: markDMReadMock,
  };
});

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: { sentinel: "test-http" },
  }),
}));

import { useReadMarker, READ_MARKER_DEBOUNCE_MS } from "./useReadMarker.js";
import { _resetAppErrorSinkForTests } from "../lib/userFacingError.js";

beforeEach(() => {
  vi.useFakeTimers();
  markChannelReadMock.mockResolvedValue(undefined);
  markDMReadMock.mockResolvedValue(undefined);
  _resetAppErrorSinkForTests();
});

afterEach(() => {
  vi.runOnlyPendingTimers();
  vi.useRealTimers();
  cleanup();
  markChannelReadMock.mockReset();
  markDMReadMock.mockReset();
  _resetAppErrorSinkForTests();
});

describe("useReadMarker", () => {
  it("debounces a single markRead call by 250ms (trailing)", () => {
    const { result } = renderHook(() => useReadMarker("channel", "C1"));
    act(() => {
      result.current.markRead("M1");
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(READ_MARKER_DEBOUNCE_MS - 1);
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(markChannelReadMock).toHaveBeenCalledTimes(1);
    expect(markChannelReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "C1", "M1");
  });

  it("collapses rapid calls into one outgoing POST with the most recent id", () => {
    const { result } = renderHook(() => useReadMarker("channel", "C1"));
    act(() => {
      result.current.markRead("M1");
      result.current.markRead("M2");
      result.current.markRead("M3");
    });
    act(() => {
      vi.advanceTimersByTime(READ_MARKER_DEBOUNCE_MS);
    });
    expect(markChannelReadMock).toHaveBeenCalledTimes(1);
    expect(markChannelReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "C1", "M3");
  });

  it("flush() fires the pending advance immediately without waiting", () => {
    const { result } = renderHook(() => useReadMarker("dm", "DM1"));
    act(() => {
      result.current.markRead("DMSG7");
    });
    expect(markDMReadMock).not.toHaveBeenCalled();
    act(() => {
      result.current.flush();
    });
    expect(markDMReadMock).toHaveBeenCalledTimes(1);
    expect(markDMReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "DM1", "DMSG7");
    // No second fire when the original timer would have elapsed.
    act(() => {
      vi.advanceTimersByTime(READ_MARKER_DEBOUNCE_MS * 2);
    });
    expect(markDMReadMock).toHaveBeenCalledTimes(1);
  });

  it("flush() with no pending advance is a no-op", () => {
    const { result } = renderHook(() => useReadMarker("channel", "C1"));
    act(() => {
      result.current.flush();
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
  });

  it("routes to markDMRead when scope is 'dm'", () => {
    const { result } = renderHook(() => useReadMarker("dm", "DM1"));
    act(() => {
      result.current.markRead("DMSG1");
      vi.advanceTimersByTime(READ_MARKER_DEBOUNCE_MS);
    });
    expect(markDMReadMock).toHaveBeenCalledTimes(1);
    expect(markDMReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "DM1", "DMSG1");
    expect(markChannelReadMock).not.toHaveBeenCalled();
  });

  it("flushes the pending advance on unmount so navigating away does not drop the pointer", () => {
    const { result, unmount } = renderHook(() => useReadMarker("channel", "C1"));
    act(() => {
      result.current.markRead("M9");
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    unmount();
    expect(markChannelReadMock).toHaveBeenCalledTimes(1);
    expect(markChannelReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "C1", "M9");
  });

  it("flushes the pending advance when document visibility returns to 'visible'", () => {
    const { result } = renderHook(() => useReadMarker("channel", "C1"));
    act(() => {
      result.current.markRead("M5");
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    act(() => {
      Object.defineProperty(document, "visibilityState", {
        configurable: true,
        get: () => "visible",
      });
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(markChannelReadMock).toHaveBeenCalledTimes(1);
    expect(markChannelReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "C1", "M5");
  });

  it("flushes the pending advance on window focus", () => {
    const { result } = renderHook(() => useReadMarker("channel", "C1"));
    act(() => {
      result.current.markRead("M6");
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    expect(markChannelReadMock).toHaveBeenCalledTimes(1);
    expect(markChannelReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "C1", "M6");
  });

  it("flushes the previous scope's pending advance when scope changes", () => {
    const { result, rerender } = renderHook(
      ({ scope, id }: { scope: "channel" | "dm"; id: string }) => useReadMarker(scope, id),
      { initialProps: { scope: "channel" as const, id: "C1" } },
    );
    act(() => {
      result.current.markRead("M1");
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    act(() => {
      rerender({ scope: "dm" as const, id: "DM1" });
    });
    expect(markChannelReadMock).toHaveBeenCalledTimes(1);
    expect(markChannelReadMock).toHaveBeenCalledWith({ sentinel: "test-http" }, "C1", "M1");
    expect(markDMReadMock).not.toHaveBeenCalled();
  });

  it("markRead is a no-op when scopeId is null (no debounce timer scheduled)", () => {
    const { result } = renderHook(() => useReadMarker("channel", null));
    act(() => {
      result.current.markRead("M1");
    });
    act(() => {
      vi.advanceTimersByTime(READ_MARKER_DEBOUNCE_MS * 2);
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
    // Subsequent flush has nothing pending → still no POST.
    act(() => {
      result.current.flush();
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
  });

  it("markRead post-flip-to-null is a no-op (rerender path)", () => {
    const { result, rerender } = renderHook(
      ({ scope, id }: { scope: "channel" | "dm"; id: string | null }) => useReadMarker(scope, id),
      { initialProps: { scope: "channel" as const, id: "C1" } },
    );
    act(() => {
      result.current.markRead("M1");
    });
    // Flip scope to null AND drain the pending cleanup flush (which
    // posts to C1 — the legitimate prior-scope flush). pendingRef is
    // now empty.
    act(() => {
      rerender({ scope: "channel" as const, id: null });
    });
    markChannelReadMock.mockReset();
    // After scope is null, any further markRead is a no-op (no timer
    // scheduled, no POST possible).
    act(() => {
      result.current.markRead("M2");
    });
    act(() => {
      vi.advanceTimersByTime(READ_MARKER_DEBOUNCE_MS * 2);
    });
    expect(markChannelReadMock).not.toHaveBeenCalled();
  });
});
