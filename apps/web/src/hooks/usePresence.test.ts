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
}

const wsTicketMock = vi.fn();
const requestMock = vi.fn();

vi.mock("../api.js", () => ({
  getClient: () => ({
    http: {
      wsTicket: wsTicketMock,
      getBaseUrl: () => "http://test.local",
      request: requestMock,
    },
  }),
}));

import { usePresence } from "./usePresence.js";

beforeEach(() => {
  (globalThis as { WebSocket?: unknown }).WebSocket = FakeSocket;
  wsTicketMock.mockResolvedValue({ ticket: "t1", expires_at: "2026-01-01T01:00:00Z" });
});

afterEach(() => {
  cleanup();
  FakeSocket.instances = [];
  delete (globalThis as { WebSocket?: unknown }).WebSocket;
  wsTicketMock.mockReset();
  requestMock.mockReset();
});

async function deliver(sock: FakeSocket | undefined, payload: unknown): Promise<void> {
  await act(async () => {
    sock?.onmessage?.({ data: JSON.stringify(payload) });
    await Promise.resolve();
  });
}

describe("usePresence", () => {
  it("seeds the list from GET /api/presence", async () => {
    requestMock.mockResolvedValue({
      users: [
        { id: "U1", username: "alice" },
        { id: "U2", username: "bob" },
      ],
    });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(requestMock).toHaveBeenCalledWith("GET", "/api/presence");
    expect(result.current.users.map((u) => u.id)).toEqual(["U1", "U2"]);
  });

  it("appends a user on a presence join frame", async () => {
    requestMock.mockResolvedValue({ users: [{ id: "U1", username: "alice" }] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, { type: "presence", data: { kind: "join", user_id: "U2" } });

    await waitFor(() => {
      expect(result.current.users.map((u) => u.id)).toEqual(["U1", "U2"]);
    });
  });

  it("removes a user on a presence leave frame", async () => {
    requestMock.mockResolvedValue({
      users: [
        { id: "U1", username: "alice" },
        { id: "U2", username: "bob" },
      ],
    });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, { type: "presence", data: { kind: "leave", user_id: "U1" } });

    await waitFor(() => {
      expect(result.current.users.map((u) => u.id)).toEqual(["U2"]);
    });
  });

  it("collapses duplicate joins for the same user_id (multi-connection dedupe)", async () => {
    requestMock.mockResolvedValue({ users: [] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, { type: "presence", data: { kind: "join", user_id: "U9" } });
    await deliver(sock, { type: "presence", data: { kind: "join", user_id: "U9" } });

    await waitFor(() => {
      expect(result.current.users).toHaveLength(1);
    });
    expect(result.current.users[0]?.id).toBe("U9");
  });

  it("ignores non-presence frames", async () => {
    requestMock.mockResolvedValue({ users: [] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, {
      type: "message",
      data: {
        id: "M1",
        channel_id: "C1",
        sender_user_id: "U1",
        body: "hi",
        created_at: "2026-01-01T00:00:00Z",
      },
    });

    expect(result.current.users).toEqual([]);
  });

  it("keeps the usernames reference stable across join/leave frames", async () => {
    requestMock.mockResolvedValue({
      users: [
        { id: "U1", username: "alice" },
        { id: "U2", username: "bob" },
      ],
    });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    const seeded = result.current.usernames;
    expect(seeded.size).toBe(2);

    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, { type: "presence", data: { kind: "join", user_id: "U3" } });
    await waitFor(() => {
      expect(result.current.users.map((u) => u.id)).toContain("U3");
    });
    expect(result.current.usernames).toBe(seeded);

    await deliver(sock, { type: "presence", data: { kind: "leave", user_id: "U1" } });
    await waitFor(() => {
      expect(result.current.users.map((u) => u.id)).not.toContain("U1");
    });
    expect(result.current.usernames).toBe(seeded);
  });

  it("preserves the usernames reference when a remount seeds an identical empty directory", async () => {
    requestMock.mockResolvedValue({ users: [] });
    const { result, rerender } = renderHook(({ on }: { on: boolean }) => usePresence(on), {
      initialProps: { on: true },
    });
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    const first = result.current.usernames;
    expect(first.size).toBe(0);

    rerender({ on: false });
    await waitFor(() => {
      expect(result.current.users).toEqual([]);
    });
    expect(result.current.usernames).toBe(first);
  });

  it("uses the frame-carried username for an unseen user (#490 join)", async () => {
    requestMock.mockResolvedValue({ users: [] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, {
      type: "presence",
      data: { kind: "join", user_id: "U_NEW", username: "Carol Newcomer" },
    });

    await waitFor(() => {
      expect(result.current.users.map((u) => u.id)).toContain("U_NEW");
    });
    expect(result.current.users.find((u) => u.id === "U_NEW")?.username).toBe("Carol Newcomer");
    expect(result.current.lastEvent).toEqual(
      expect.objectContaining({ kind: "join", id: "U_NEW", username: "Carol Newcomer" }),
    );
    expect(result.current.usernames.get("U_NEW")).toBe("Carol Newcomer");
  });

  it("prefers the frame-carried username over the seeded directory (#490)", async () => {
    requestMock.mockResolvedValue({ users: [{ id: "U1", username: "stale-name" }] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, {
      type: "presence",
      data: { kind: "join", user_id: "U1", username: "fresh-name" },
    });

    await waitFor(() => {
      expect(result.current.lastEvent?.username).toBe("fresh-name");
    });
    expect(result.current.usernames.get("U1")).toBe("fresh-name");
  });

  it("falls back to the seeded directory when the server omits username (#490 backwards compat)", async () => {
    requestMock.mockResolvedValue({ users: [{ id: "U1", username: "alice" }] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    // Drop U1 first so the seed-directory fallback is the only source.
    await deliver(sock, { type: "presence", data: { kind: "leave", user_id: "U1" } });

    await waitFor(() => {
      expect(result.current.lastEvent?.kind).toBe("leave");
    });
    expect(result.current.lastEvent?.username).toBe("alice");
  });

  it("emits the frame-carried username on a leave for a never-seeded id (#490)", async () => {
    requestMock.mockResolvedValue({ users: [] });
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(FakeSocket.instances).toHaveLength(1);
    });
    const sock = FakeSocket.instances[0];
    sock?.open();

    await deliver(sock, {
      type: "presence",
      data: { kind: "leave", user_id: "U_GONE", username: "Dora Departed" },
    });

    await waitFor(() => {
      expect(result.current.lastEvent?.id).toBe("U_GONE");
    });
    expect(result.current.lastEvent?.username).toBe("Dora Departed");
  });

  it("surfaces a curated error when the seed request fails without echoing the raw err.message", async () => {
    const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const raw = new Error("seed boom internal-trace-xyz");
    requestMock.mockRejectedValue(raw);
    const { result } = renderHook(() => usePresence(true));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    // Curated copy carries the prefix; the raw err.message must not surface.
    expect(result.current.error).toBe("Failed to load presence: Something went wrong.");
    expect(result.current.error).not.toContain("seed boom");
    expect(result.current.error).not.toContain("internal-trace-xyz");
    expect(consoleErrorSpy).toHaveBeenCalledWith("Failed to load presence", raw);
    consoleErrorSpy.mockRestore();
  });
});
