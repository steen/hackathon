import { describe, expect, it } from "vitest";
import { ApiError, type Message } from "@hackathon/api-client";
import {
  classifyError,
  findPendingMatch,
  makePendingRow,
  markPendingFailed,
  type MessageView,
  mergeFetchedCatchup,
  newPendingId,
  oldestCommittedId,
  type PendingMeta,
  prependOlderPage,
  RECONCILE_WINDOW_MS,
  REASON_GENERIC,
  REASON_NETWORK,
  REASON_SERVER_UNAVAILABLE,
  REASON_SESSION_INVALID,
  REASON_TIMEOUT,
  reconcilePersisted,
  reconcileWsMessage,
  startRetry,
} from "./useMessages.helpers.js";

const ME = "U1";

function realMsg(id: string, body: string, createdAt = "2026-01-01T00:00:00.000Z"): Message {
  return { id, channel_id: "C1", sender_user_id: ME, body, created_at: createdAt };
}

function pendingRow(id: string, body: string): MessageView {
  return makePendingRow(id, "C1", ME, body);
}

function metaMap(...entries: [string, number][]): Map<string, PendingMeta> {
  return new Map(entries.map(([id, t]) => [id, { submittedAt: t }]));
}

describe("findPendingMatch", () => {
  it("matches pending with same body inside the reconcile window", () => {
    const submitted = Date.parse("2026-01-01T00:00:05.000Z");
    const messages = [pendingRow("p1", "hello")];
    const meta = metaMap(["p1", submitted]);
    const frame = realMsg("M1", "hello", "2026-01-01T00:00:08.000Z");
    const match = findPendingMatch(messages, frame, meta, RECONCILE_WINDOW_MS);
    expect(match).toEqual({ index: 0, pendingId: "p1" });
  });

  it("still matches outside the window — FIFO fallback for clock skew", () => {
    // Window is 10s; place the frame 60s after submit. The hook's logic
    // intentionally falls back to FIFO body+sender when the window is
    // breached so frozen-clock test fixtures still reconcile.
    const submitted = Date.parse("2026-01-01T00:00:00.000Z");
    const messages = [pendingRow("p1", "hello")];
    const meta = metaMap(["p1", submitted]);
    const frame = realMsg("M1", "hello", "2026-01-01T00:01:00.000Z");
    const match = findPendingMatch(messages, frame, meta, RECONCILE_WINDOW_MS);
    expect(match).toEqual({ index: 0, pendingId: "p1" });
  });

  it("returns null when bodies differ", () => {
    const submitted = Date.parse("2026-01-01T00:00:00.000Z");
    const messages = [pendingRow("p1", "hello")];
    const meta = metaMap(["p1", submitted]);
    const frame = realMsg("M1", "different", "2026-01-01T00:00:01.000Z");
    expect(findPendingMatch(messages, frame, meta, RECONCILE_WINDOW_MS)).toBeNull();
  });

  it("returns null when pending list is empty", () => {
    const frame = realMsg("M1", "hello");
    expect(findPendingMatch([], frame, new Map(), RECONCILE_WINDOW_MS)).toBeNull();
  });

  it("prefers an in-window candidate over an out-of-window one", () => {
    const wsAt = Date.parse("2026-01-01T00:00:10.000Z");
    const inWindow = wsAt - 2_000;
    const outOfWindow = wsAt - 60_000;
    const messages = [pendingRow("p-old", "same"), pendingRow("p-new", "same")];
    const meta = metaMap(["p-old", outOfWindow], ["p-new", inWindow]);
    const frame = realMsg("M1", "same", "2026-01-01T00:00:10.000Z");
    const match = findPendingMatch(messages, frame, meta, RECONCILE_WINDOW_MS);
    expect(match).toEqual({ index: 1, pendingId: "p-new" });
  });

  it("among same-window-class candidates, picks the oldest submittedAt (FIFO)", () => {
    const wsAt = Date.parse("2026-01-01T00:00:05.000Z");
    const messages = [pendingRow("p-newer", "same"), pendingRow("p-older", "same")];
    const meta = metaMap(["p-newer", wsAt - 1_000], ["p-older", wsAt - 4_000]);
    const frame = realMsg("M1", "same", "2026-01-01T00:00:05.000Z");
    const match = findPendingMatch(messages, frame, meta, RECONCILE_WINDOW_MS);
    expect(match).toEqual({ index: 1, pendingId: "p-older" });
  });

  it("treats unparseable created_at as out-of-window (still FIFO-matches)", () => {
    const messages = [pendingRow("p1", "hello")];
    const meta = metaMap(["p1", Date.now()]);
    const frame: Message = { ...realMsg("M1", "hello"), created_at: "not-a-date" };
    expect(findPendingMatch(messages, frame, meta, RECONCILE_WINDOW_MS)).toEqual({
      index: 0,
      pendingId: "p1",
    });
  });

  it("skips pending rows missing meta entries", () => {
    const messages = [pendingRow("p1", "hello")];
    const frame = realMsg("M1", "hello");
    // Empty meta — defensive guard returns null rather than matching.
    expect(findPendingMatch(messages, frame, new Map(), RECONCILE_WINDOW_MS)).toBeNull();
  });
});

describe("reconcileWsMessage", () => {
  it("transitions a pending row to confirmed by replacing in place", () => {
    const submitted = Date.parse("2026-01-01T00:00:00.000Z");
    const prev: MessageView[] = [
      {
        id: "x",
        channel_id: "C1",
        sender_user_id: "U2",
        body: "hi",
        created_at: "2026-01-01T00:00:00.000Z",
      },
      pendingRow("p1", "hello"),
    ];
    const meta = metaMap(["p1", submitted]);
    const frame = realMsg("M1", "hello", "2026-01-01T00:00:01.000Z");
    const result = reconcileWsMessage(prev, frame, ME, meta, RECONCILE_WINDOW_MS);
    expect(result.matchedPendingId).toBe("p1");
    expect(result.next).toHaveLength(2);
    expect(result.next[1]?.id).toBe("M1");
    expect(result.next[1]?.body).toBe("hello");
  });

  it("appends a confirmed-only frame from another sender", () => {
    const prev: MessageView[] = [];
    const frame: Message = { ...realMsg("M1", "hello"), sender_user_id: "U-other" };
    const result = reconcileWsMessage(prev, frame, ME, new Map(), RECONCILE_WINDOW_MS);
    expect(result.matchedPendingId).toBeNull();
    expect(result.next).toEqual([frame]);
  });

  it("preserves a failed pending row when an unrelated frame arrives", () => {
    const failed: MessageView = {
      ...pendingRow("p1", "hello"),
      status: "failed",
      failureReason: "boom",
    };
    const frame: Message = { ...realMsg("M2", "different"), sender_user_id: "U-other" };
    const result = reconcileWsMessage([failed], frame, ME, new Map(), RECONCILE_WINDOW_MS);
    expect(result.next[0]).toBe(failed);
    expect(result.next).toHaveLength(2);
  });

  it("preserves out-of-order frame ordering — newer frame appends after older ones", () => {
    const prev: MessageView[] = [
      realMsg("M1", "first", "2026-01-01T00:00:00.000Z"),
      realMsg("M2", "second", "2026-01-01T00:00:01.000Z"),
    ];
    // A frame for an older time still appends to the end — the hook
    // does not re-sort by created_at; insertion is FIFO by arrival.
    const lateFrame = realMsg("M0", "earlier-but-arrived-late", "2025-12-31T23:59:59.000Z");
    const result = reconcileWsMessage(prev, lateFrame, ME, new Map(), RECONCILE_WINDOW_MS);
    expect(result.next.map((m) => m.id)).toEqual(["M1", "M2", "M0"]);
  });

  it("drops a duplicate-id frame (REST already swapped the row)", () => {
    const persisted = realMsg("M1", "hello");
    const result = reconcileWsMessage([persisted], persisted, ME, new Map(), RECONCILE_WINDOW_MS);
    expect(result.matchedPendingId).toBeNull();
    expect(result.next).toEqual([persisted]);
  });

  it("does not match pending rows when sender differs from the current user", () => {
    const submitted = Date.parse("2026-01-01T00:00:00.000Z");
    const prev = [pendingRow("p1", "hello")];
    const meta = metaMap(["p1", submitted]);
    // Frame's sender_user_id matches the pending row's sender, but the
    // hook passes currentUserId !== that sender → reconcile is skipped.
    const frame = realMsg("M1", "hello", "2026-01-01T00:00:01.000Z");
    const result = reconcileWsMessage(prev, frame, "someone-else", meta, RECONCILE_WINDOW_MS);
    expect(result.matchedPendingId).toBeNull();
    expect(result.next).toHaveLength(2);
  });
});

describe("classifyError", () => {
  it("maps 401/403 ApiError → SESSION_INVALID", () => {
    expect(classifyError(new ApiError(401, "unauthorized", "msg"))).toBe(REASON_SESSION_INVALID);
    expect(classifyError(new ApiError(403, "forbidden", "msg"))).toBe(REASON_SESSION_INVALID);
  });

  it("maps 5xx ApiError → SERVER_UNAVAILABLE", () => {
    expect(classifyError(new ApiError(500, "boom", "msg"))).toBe(REASON_SERVER_UNAVAILABLE);
    expect(classifyError(new ApiError(503, "down", "msg"))).toBe(REASON_SERVER_UNAVAILABLE);
  });

  it("maps 408 ApiError → TIMEOUT", () => {
    expect(classifyError(new ApiError(408, "timeout", "msg"))).toBe(REASON_TIMEOUT);
  });

  it("maps fetch's TypeError (network/DNS/refused) → NETWORK", () => {
    expect(classifyError(new TypeError("fetch failed"))).toBe(REASON_NETWORK);
  });

  it("maps a server-envelope-error ApiError (4xx other) → GENERIC", () => {
    expect(classifyError(new ApiError(400, "bad request", "msg"))).toBe(REASON_GENERIC);
  });

  it("maps unknown values → GENERIC", () => {
    expect(classifyError("some string")).toBe(REASON_GENERIC);
    expect(classifyError(undefined)).toBe(REASON_GENERIC);
    expect(classifyError(new Error("vanilla"))).toBe(REASON_GENERIC);
  });
});

describe("mergeFetchedCatchup", () => {
  it("returns null when fetched is empty", () => {
    expect(mergeFetchedCatchup([realMsg("M1", "a")], [])).toBeNull();
  });

  it("returns null when every fetched id is already present", () => {
    const m1 = realMsg("M1", "a");
    expect(mergeFetchedCatchup([m1], [m1])).toBeNull();
  });

  it("appends fresh entries in oldest→newest order (server returns newest-first)", () => {
    const existing = realMsg("M1", "first");
    // Server response is newest-first.
    const fetched = [realMsg("M3", "third"), realMsg("M2", "second")];
    const next = mergeFetchedCatchup([existing], fetched);
    expect(next?.map((m) => m.id)).toEqual(["M1", "M2", "M3"]);
  });
});

describe("reconcilePersisted", () => {
  it("replaces the pending row in place with the persisted message", () => {
    const prev: MessageView[] = [pendingRow("p1", "hello")];
    const persisted = realMsg("M1", "hello");
    const next = reconcilePersisted(prev, "p1", persisted);
    expect(next).toEqual([persisted]);
  });

  it("drops the pending row if the persisted id already landed (WS beat REST)", () => {
    const persisted = realMsg("M1", "hello");
    const prev: MessageView[] = [persisted, pendingRow("p1", "hello")];
    const next = reconcilePersisted(prev, "p1", persisted);
    expect(next).toEqual([persisted]);
  });
});

describe("markPendingFailed / startRetry", () => {
  it("flips the pending row to failed and stamps the reason", () => {
    const prev: MessageView[] = [pendingRow("p1", "hello"), pendingRow("p2", "world")];
    const next = markPendingFailed(prev, "p1", "Could not reach the server");
    expect(next[0]).toMatchObject({
      id: "p1",
      status: "failed",
      failureReason: "Could not reach the server",
    });
    expect(next[1]).toBe(prev[1]);
  });

  it("startRetry returns the body and clears the failureReason", () => {
    const failed: MessageView = {
      ...pendingRow("p1", "hello"),
      status: "failed",
      failureReason: "boom",
    };
    const { next, body } = startRetry([failed], "p1");
    expect(body).toBe("hello");
    expect(next[0]).toMatchObject({ id: "p1", status: "pending", failureReason: undefined });
  });

  it("startRetry returns body=undefined when the id no longer matches", () => {
    const { body } = startRetry([], "p-missing");
    expect(body).toBeUndefined();
  });
});

describe("oldestCommittedId / prependOlderPage", () => {
  it("oldestCommittedId skips pending and failed rows", () => {
    const failed: MessageView = { ...pendingRow("p1", "hi"), status: "failed" };
    const real = realMsg("M1", "hello");
    expect(oldestCommittedId([failed, pendingRow("p2", "x"), real])).toBe("M1");
  });

  it("oldestCommittedId returns undefined when no committed rows exist", () => {
    expect(oldestCommittedId([pendingRow("p1", "x")])).toBeUndefined();
  });

  it("prependOlderPage reverses (server is newest-first) and dedups", () => {
    const existing = realMsg("M3", "third");
    const page = [realMsg("M2", "second"), realMsg("M1", "first")];
    const next = prependOlderPage([existing], page);
    expect(next?.map((m) => m.id)).toEqual(["M1", "M2", "M3"]);
  });

  it("prependOlderPage returns null when nothing is fresh", () => {
    const m = realMsg("M1", "x");
    expect(prependOlderPage([m], [m])).toBeNull();
  });
});

describe("newPendingId", () => {
  it("returns a string with the pending- prefix", () => {
    const id = newPendingId();
    expect(id.startsWith("pending-")).toBe(true);
    expect(id.length).toBeGreaterThan("pending-".length);
  });

  it("is unique across calls", () => {
    const ids = new Set<string>();
    for (let i = 0; i < 16; i += 1) ids.add(newPendingId());
    expect(ids.size).toBe(16);
  });
});
