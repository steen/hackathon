import { useEffect, useRef, useState } from "react";
import { WebSocketClient, type Event as WsEvent, type Message } from "@hackathon/api-client";
import { getClient } from "../api.js";

export type ConnectionState = "idle" | "connecting" | "open" | "closed" | "reconnecting";

interface UseMessages {
  messages: Message[];
  connection: ConnectionState;
  error: string | null;
}

const BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000];

interface CancelToken {
  cancelled: boolean;
}

// Number of messages to refetch on a WS reopen. Larger than a typical
// outage burst but bounded so the request stays cheap. The server caps
// this at MaxMessagesLimit anyway.
const CATCHUP_LIMIT = 50;

export function useMessages(channelId: string | null): UseMessages {
  const [messages, setMessages] = useState<Message[]>([]);
  const [connection, setConnection] = useState<ConnectionState>("idle");
  const [error, setError] = useState<string | null>(null);
  const wsRef = useRef<WebSocketClient | null>(null);

  useEffect(() => {
    if (channelId === null) {
      setMessages([]);
      setConnection("idle");
      return;
    }
    const tok: CancelToken = { cancelled: false };
    setMessages([]);
    setError(null);
    setConnection("connecting");

    // openCount distinguishes the initial WS open (which already has
    // a fresh history fetch) from later reopens (which must catch up
    // anything posted while the socket was down). Held in a ref so
    // the listener closure sees mutations without re-binding.
    let openCount = 0;

    const mergeFetched = (fetched: Message[]): void => {
      if (tok.cancelled) return;
      setMessages((prev) => {
        if (fetched.length === 0) return prev;
        const seen = new Set(prev.map((p) => p.id));
        const fresh = fetched.filter((m) => !seen.has(m.id));
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
          // A failed catchup leaves the list as-is; the user can scroll
          // away and back to force a full refetch. Surfacing the error
          // would also clobber a still-valid connection state.
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

  return { messages, connection, error };
}

export { BACKOFF_MS };
