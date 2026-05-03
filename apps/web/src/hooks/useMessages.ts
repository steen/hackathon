import { useCallback, useEffect, useRef, useState } from "react";
import { WebSocketClient, type Event as WsEvent, type Message } from "@hackathon/api-client";
import { getClient } from "../api.js";

export type ConnectionState = "idle" | "connecting" | "open" | "closed" | "reconnecting";

export type MessageStatus = "pending" | "failed";

export interface MessageView extends Message {
  status?: MessageStatus;
}

interface PendingMeta {
  submittedAt: number;
}

interface UseMessages {
  messages: MessageView[];
  connection: ConnectionState;
  error: string | null;
  send: (body: string) => Promise<void>;
  retry: (pendingId: string) => Promise<void>;
}

const BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000];

interface CancelToken {
  cancelled: boolean;
}

const CATCHUP_LIMIT = 50;

// Reconcile window: when both sides' clocks agree, a WS frame must land
// within this many ms of the local submit timestamp to fold onto a pending
// entry. If wall clocks differ by more than this (test fixtures with frozen
// dates, severe client drift), fall back to FIFO matching by body+sender —
// the strict gate is only useful for telling apart back-to-back identical
// sends on a healthy clock.
const RECONCILE_WINDOW_MS = 10_000;

function newPendingId(): string {
  // jsdom + modern browsers provide crypto.randomUUID. Fall back to a
  // time+random string only if the runtime lacks it (older test stubs).
  const c: { randomUUID?: () => string } | undefined = (
    globalThis as { crypto?: { randomUUID?: () => string } }
  ).crypto;
  const uuid =
    typeof c?.randomUUID === "function"
      ? c.randomUUID()
      : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  return `pending-${uuid}`;
}

export function useMessages(channelId: string | null, currentUserId?: string | null): UseMessages {
  const [messages, setMessages] = useState<MessageView[]>([]);
  const [connection, setConnection] = useState<ConnectionState>("idle");
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocketClient | null>(null);
  // Tracks original submit time per pending id, so the reconcile path can
  // bound the WS-frame match to a recent window without leaking timestamps
  // into the rendered MessageView.
  const pendingMetaRef = useRef<Map<string, PendingMeta>>(new Map());
  const userIdRef = useRef<string | null>(currentUserId ?? null);
  userIdRef.current = currentUserId ?? null;
  const channelIdRef = useRef<string | null>(channelId);
  channelIdRef.current = channelId;

  useEffect(() => {
    if (channelId === null) {
      setMessages([]);
      setConnection("idle");
      pendingMetaRef.current.clear();
      return;
    }
    const tok: CancelToken = { cancelled: false };
    setMessages([]);
    setError(null);
    setConnection("connecting");
    pendingMetaRef.current.clear();

    let openCount = 0;

    const mergeFetched = (fetched: Message[]): void => {
      if (tok.cancelled) return;
      setMessages((prev) => {
        if (fetched.length === 0) return prev;
        const seen = new Set(prev.map((p) => p.id));
        const fresh = fetched.filter((m) => !seen.has(m.id)).reverse();
        if (fresh.length === 0) return prev;
        return [...prev, ...fresh];
      });
    };

    const catchup = (): void => {
      void (async () => {
        try {
          const recent = await getClient().listMessages(channelId, { limit: CATCHUP_LIMIT });
          mergeFetched(recent);
        } catch {
          /* see history-failure note below */
        }
      })();
    };

    /* eslint-disable @typescript-eslint/no-unnecessary-condition --
       tok.cancelled is mutated by the effect cleanup closure; eslint's
       flow analysis can't see the cross-closure write, so flags every
       check as "always falsy". */
    void (async () => {
      try {
        const history = await getClient().listMessages(channelId, { limit: CATCHUP_LIMIT });
        if (tok.cancelled) return;
        setMessages(history);
      } catch (err) {
        if (tok.cancelled) return;
        const msg = err instanceof Error ? err.message : "failed to load history";
        setError(msg);
      }

      if (tok.cancelled) return;

      const ws = new WebSocketClient({
        http: getClient().http,
        channelId,
        backoffMs: BACKOFF_MS,
      });
      wsRef.current = ws;
      ws.on("open", () => {
        if (tok.cancelled) return;
        setConnection("open");
        openCount += 1;
        if (openCount > 1) catchup();
      });
      ws.on("close", () => {
        if (!tok.cancelled) setConnection("reconnecting");
      });
      ws.on("error", () => {
        if (!tok.cancelled) setConnection("reconnecting");
      });
      ws.on("message", (ev: WsEvent) => {
        if (tok.cancelled) return;
        if (ev.type === "message") {
          const m = ev.data as Message;
          setMessages((prev) => {
            if (prev.some((p) => p.id === m.id)) return prev;
            // Reconcile: if this frame is an echo of a still-pending local
            // send, swap the pending entry in place rather than appending a
            // duplicate.
            const me = userIdRef.current;
            if (me !== null && m.sender_user_id === me) {
              const wsAt = Date.parse(m.created_at);
              interface Candidate {
                idx: number;
                submittedAt: number;
                inWindow: boolean;
              }
              let best: Candidate | null = null;
              for (let i = 0; i < prev.length; i += 1) {
                const p = prev[i];
                if (p === undefined) continue;
                if (p.status !== "pending") continue;
                if (p.body !== m.body) continue;
                const meta = pendingMetaRef.current.get(p.id);
                if (meta === undefined) continue;
                const inWindow =
                  !Number.isNaN(wsAt) && Math.abs(wsAt - meta.submittedAt) <= RECONCILE_WINDOW_MS;
                // Prefer in-window matches, then oldest submitted (FIFO).
                if (
                  best === null ||
                  (inWindow && !best.inWindow) ||
                  (inWindow === best.inWindow && meta.submittedAt < best.submittedAt)
                ) {
                  best = { idx: i, submittedAt: meta.submittedAt, inWindow };
                }
              }
              if (best !== null) {
                const next = prev.slice();
                const pendingEntry = next[best.idx];
                if (pendingEntry !== undefined) {
                  pendingMetaRef.current.delete(pendingEntry.id);
                }
                next[best.idx] = m;
                return next;
              }
            }
            return [...prev, m];
          });
        }
      });
      try {
        await ws.connect();
      } catch (err) {
        if (tok.cancelled) return;
        const msg = err instanceof Error ? err.message : "websocket failed";
        setError(msg);
        setConnection("reconnecting");
      }
    })();
    /* eslint-enable @typescript-eslint/no-unnecessary-condition */

    return () => {
      tok.cancelled = true;
      const ws = wsRef.current;
      if (ws !== null) {
        ws.close();
        wsRef.current = null;
      }
      setConnection("closed");
    };
  }, [channelId]);

  const submitPending = useCallback(async (id: string, ch: string, body: string): Promise<void> => {
    pendingMetaRef.current.set(id, { submittedAt: Date.now() });
    try {
      await getClient().postMessage(ch, body);
      // Success path: do nothing here. The reconciliation in the WS
      // "message" handler swaps the pending entry for the persisted one
      // when the server's frame arrives.
    } catch {
      // Keep the entry but mark it failed so the user can retry. Channel-
      // level `error` stays reserved for history/socket failures; the per-
      // entry `failed` status is what surfaces to the user.
      setMessages((prev) => prev.map((p) => (p.id === id ? { ...p, status: "failed" } : p)));
    }
  }, []);

  const send = useCallback(
    async (body: string): Promise<void> => {
      const ch = channelIdRef.current;
      if (ch === null) return;
      const trimmed = body.trim();
      if (trimmed.length === 0) return;
      const id = newPendingId();
      const me = userIdRef.current ?? "";
      const synthetic: MessageView = {
        id,
        channel_id: ch,
        sender_user_id: me,
        body: trimmed,
        created_at: "",
        status: "pending",
      };
      setMessages((prev) => [...prev, synthetic]);
      setError(null);
      await submitPending(id, ch, trimmed);
    },
    [submitPending],
  );

  const retry = useCallback(
    async (pendingId: string): Promise<void> => {
      const ch = channelIdRef.current;
      if (ch === null) return;
      // setMessages's reducer can't read state out, so capture the body via
      // a closure-mutable holder. The first character of the holder string
      // signals "not found"; any pending entry's body is the stored value.
      const found: { body: string | undefined } = { body: undefined };
      setMessages((prev) =>
        prev.map((p) => {
          if (p.id !== pendingId) return p;
          found.body = p.body;
          return { ...p, status: "pending" };
        }),
      );
      if (found.body === undefined) return;
      setError(null);
      await submitPending(pendingId, ch, found.body);
    },
    [submitPending],
  );

  return { messages, connection, error, send, retry };
}

export { BACKOFF_MS };
