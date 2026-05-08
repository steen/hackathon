import type { Message } from "@hackathon/api-client";
import type { MessageStatus } from "@hackathon/chat-ui";

// Pure helpers extracted from useMessages.ts so the reconciliation math is
// readable and unit-testable in isolation. None of these functions read or
// mutate refs, state, the DOM, or `window`; every input is an argument and
// every output is the next value the hook should commit. Side-effecting
// glue (refs, setState, network) stays in the hook.

export interface MessageView extends Message {
  status?: MessageStatus;
  failureReason?: string;
}

export interface PendingMeta {
  submittedAt: number;
}

export interface PendingMatch {
  index: number;
  pendingId: string;
}

export const CATCHUP_LIMIT = 50;

// Reconcile window: a WS frame must land within this many ms of the
// local submit timestamp to fold onto a pending entry on a healthy
// clock. Outside the window, FIFO body+sender matching still applies —
// this gate only disambiguates back-to-back identical sends.
export const RECONCILE_WINDOW_MS = 10_000;

/**
 * Generate a fresh client-side id for an optimistic-pending row.
 * jsdom + modern browsers expose `crypto.randomUUID`; older test stubs
 * fall through to a time+random string.
 */
export function newPendingId(): string {
  const c: { randomUUID?: () => string } | undefined = (
    globalThis as { crypto?: { randomUUID?: () => string } }
  ).crypto;
  const uuid =
    typeof c?.randomUUID === "function"
      ? c.randomUUID()
      : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  return `pending-${uuid}`;
}

// classifyError already lives in lib/userFacingError.ts as the curated
// error → REASON_* mapper used by both hooks and forms; re-export here so
// callers that pull "all message-reconciliation helpers" from one module
// (and the helper-suite tests) can reach it without two imports.
export {
  classifyError,
  REASON_NETWORK,
  REASON_SERVER_UNAVAILABLE,
  REASON_SESSION_INVALID,
  REASON_TIMEOUT,
  REASON_GENERIC,
} from "../lib/userFacingError.js";

/**
 * Build a synthetic optimistic-pending row for a fresh outgoing send.
 * `created_at` is left blank — the WS echo or REST response carries the
 * server-assigned timestamp.
 */
export function makePendingRow(
  pendingId: string,
  channelId: string,
  senderUserId: string,
  body: string,
): MessageView {
  return {
    id: pendingId,
    channel_id: channelId,
    sender_user_id: senderUserId,
    body,
    created_at: "",
    status: "pending",
  };
}

/**
 * Find a pending optimistic-send entry that the given WS frame should
 * reconcile onto. Matching is body-equality first; among same-body
 * candidates, in-window matches (|wsAt − submittedAt| ≤ `windowMs`) beat
 * out-of-window ones, and within the same window-class FIFO wins (oldest
 * `submittedAt`). Pure: never reads refs or state.
 *
 * Inputs:
 *   - `messages`: current message list (the hook's prev state slice).
 *   - `frame`: the incoming WS message frame.
 *   - `pendingMeta`: per-pendingId submitted-at timestamps; entries
 *     without metadata are skipped (a defensive guard, not expected in
 *     practice).
 *   - `windowMs`: the reconcile window in ms.
 *
 * Returns the winning candidate's `{ index, pendingId }` or `null` when
 * no pending entry has the same body. `Number.isNaN(wsAt)` (unparseable
 * `created_at`) collapses to "out of window" rather than dropping the
 * candidate, so the FIFO fallback still applies.
 */
export function findPendingMatch(
  messages: readonly MessageView[],
  frame: Message,
  pendingMeta: ReadonlyMap<string, PendingMeta>,
  windowMs: number,
): PendingMatch | null {
  const wsAt = Date.parse(frame.created_at);
  interface Candidate {
    index: number;
    pendingId: string;
    submittedAt: number;
    inWindow: boolean;
  }
  let best: Candidate | null = null;
  for (let i = 0; i < messages.length; i += 1) {
    const p = messages[i];
    if (p === undefined) continue;
    if (p.status !== "pending") continue;
    if (p.body !== frame.body) continue;
    const meta = pendingMeta.get(p.id);
    if (meta === undefined) continue;
    const inWindow = !Number.isNaN(wsAt) && Math.abs(wsAt - meta.submittedAt) <= windowMs;
    if (
      best === null ||
      (inWindow && !best.inWindow) ||
      (inWindow === best.inWindow && meta.submittedAt < best.submittedAt)
    ) {
      best = { index: i, pendingId: p.id, submittedAt: meta.submittedAt, inWindow };
    }
  }
  if (best === null) return null;
  return { index: best.index, pendingId: best.pendingId };
}

export interface ReconcileResult {
  next: MessageView[];
  /** Pending id whose meta should be evicted, or null when no swap occurred. */
  matchedPendingId: string | null;
}

/**
 * Apply an incoming WS message frame to current state and return the
 * next message list plus any pending-meta key that should be evicted.
 * Three branches:
 *   1. Frame's id already in state → return prev unchanged (REST-side
 *      reconcile already swapped the row; WS echo is a duplicate).
 *   2. Frame is the user's own send and matches a pending entry → swap
 *      in place at the matched index, return its pendingId for eviction.
 *   3. Otherwise append to the end.
 *
 * Pure: takes the meta map by reference but does not mutate it. The
 * caller deletes the returned `matchedPendingId` from its ref-held map.
 */
export function reconcileWsMessage(
  prev: readonly MessageView[],
  frame: Message,
  currentUserId: string | null,
  pendingMeta: ReadonlyMap<string, PendingMeta>,
  windowMs: number,
): ReconcileResult {
  if (prev.some((p) => p.id === frame.id)) {
    return { next: prev.slice(), matchedPendingId: null };
  }
  if (currentUserId !== null && frame.sender_user_id === currentUserId) {
    const match = findPendingMatch(prev, frame, pendingMeta, windowMs);
    if (match !== null) {
      const next = prev.slice();
      next[match.index] = frame;
      return { next, matchedPendingId: match.pendingId };
    }
  }
  return { next: [...prev, frame], matchedPendingId: null };
}

/**
 * Merge a freshly-fetched catchup page into current state. Server
 * returns newest-first; the view holds oldest→newest, so we reverse the
 * fresh tail and append. Entries already present (matched by id) are
 * skipped. Returns `prev` unchanged when nothing is new, so callers can
 * use referential equality as a no-op signal.
 */
export function mergeFetchedCatchup(
  prev: readonly MessageView[],
  fetched: readonly Message[],
): MessageView[] | null {
  if (fetched.length === 0) return null;
  const seen = new Set(prev.map((p) => p.id));
  const fresh = fetched.filter((m) => !seen.has(m.id)).reverse();
  if (fresh.length === 0) return null;
  return [...prev, ...fresh];
}

/**
 * Reconcile a REST-postMessage response onto the optimistic pending row
 * with id `pendingId`. If the persisted message's id already exists in
 * state (the WS echo got there first), drop the pending row to avoid a
 * duplicate. Otherwise replace the pending row in place with the
 * persisted Message.
 */
export function reconcilePersisted(
  prev: readonly MessageView[],
  pendingId: string,
  persisted: Message,
): MessageView[] {
  if (prev.some((p) => p.id === persisted.id)) {
    return prev.filter((p) => p.id !== pendingId);
  }
  return prev.map((p) => (p.id === pendingId ? persisted : p));
}

/**
 * Mark a pending row failed and stamp the curated reason. Other rows are
 * preserved by reference (no shallow churn beyond the matched id).
 */
export function markPendingFailed(
  prev: readonly MessageView[],
  pendingId: string,
  reason: string,
): MessageView[] {
  return prev.map((p) =>
    p.id === pendingId ? { ...p, status: "failed", failureReason: reason } : p,
  );
}

/**
 * Flip a failed row back to pending and clear its failureReason ahead of
 * a retry. Returns both the next list and the body the caller needs to
 * resubmit (or `undefined` when the id no longer matches a row).
 */
export function startRetry(
  prev: readonly MessageView[],
  pendingId: string,
): { next: MessageView[]; body: string | undefined } {
  let body: string | undefined;
  const next = prev.map((p) => {
    if (p.id !== pendingId) return p;
    body = p.body;
    return { ...p, status: "pending" as const, failureReason: undefined };
  });
  return { next, body };
}

/**
 * Return the id of the oldest "real" (non-pending, non-failed) row, or
 * `undefined` when state has no committed rows yet. Used as the `before`
 * cursor for loadOlder.
 */
export function oldestCommittedId(messages: readonly MessageView[]): string | undefined {
  for (const m of messages) {
    if (m.status === "pending" || m.status === "failed") continue;
    return m.id;
  }
  return undefined;
}

/**
 * Prepend an older page (server returns newest-first; we reverse to
 * oldest→newest before prepending) onto current state, deduping against
 * `prev`. Returns `null` when no row would actually be added — caller
 * should leave state untouched (avoids a no-op render under StrictMode).
 */
export function prependOlderPage(
  prev: readonly MessageView[],
  page: readonly Message[],
): MessageView[] | null {
  const reversed = [...page].reverse();
  const seen = new Set(prev.map((p) => p.id));
  const fresh = reversed.filter((m) => !seen.has(m.id));
  if (fresh.length === 0) return null;
  return [...fresh, ...prev];
}
