import { useEffect, useRef, useState } from "react";
import { WebSocketClient, type Event as WsEvent } from "@hackathon/api-client";
import type { ConnectionStatus } from "@hackathon/chat-ui";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";

// Owner of the chat-page WebSocket connection. Lifted out of useMessages
// so additional consumers (channel-create/rename frames, presence) can
// share the same socket without each constructing their own. This file
// owns: ticket fetch, WebSocketClient construction, reconnect cadence,
// and connection-state surface; consumers attach typed listeners via
// `subscribe`. No reconnect/backoff change vs. the prior in-line shape —
// schedule is still BACKOFF_MS, behavior is still WebSocketClient's.
//
// The hook constructs at most one WebSocketClient per (channelId !== null)
// lifecycle. When channelId flips to null, the socket closes and the
// connection state collapses to "idle". When channelId changes to a new
// value, the previous socket closes before the new one opens.

export const BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000];

export type ChatSocketEventName = "open" | "close" | "error" | "message";

export interface ChatSocketEventMap {
  open: undefined;
  close: { code: number; reason: string };
  error: unknown;
  message: WsEvent;
}

export type ChatSocketListener<E extends ChatSocketEventName> = (
  arg: ChatSocketEventMap[E],
) => void;

export interface ChatSocket {
  /** Subscribe to a typed WebSocket event. Returns the unsubscribe fn. */
  subscribe: <E extends ChatSocketEventName>(event: E, fn: ChatSocketListener<E>) => () => void;
}

export interface UseChatSocket {
  /** Connection status mirroring the chat-ui ConnectionStatus surface. */
  connection: ConnectionStatus;
  /** Curated banner for connection failures (currently: ticket fetch error). */
  error: string | null;
  /**
   * Subscribe API. Stable across renders for the same channel — handlers
   * attached once survive reconnects. Returns the unsubscribe fn so
   * consumers don't leak listeners on unmount.
   */
  socket: ChatSocket;
}

/**
 * Shared WebSocket lifecycle hook. Returns connection state plus a
 * subscribe helper. Pure refactor of the WS plumbing previously inlined
 * in useMessages.connect — same WebSocketClient, same backoff schedule,
 * same idle/connecting/open/reconnecting/closed states.
 */
export function useChatSocket(channelId: string | null): UseChatSocket {
  const [connection, setConnection] = useState<ConnectionStatus>("idle");
  const [error, setError] = useState<string | null>(null);

  // Listener registries are keyed by event name. We hold them in refs so
  // subscribe() returned to consumers can attach/detach without re-running
  // the connect effect.
  const listenersRef = useRef<{
    open: Set<ChatSocketListener<"open">>;
    close: Set<ChatSocketListener<"close">>;
    error: Set<ChatSocketListener<"error">>;
    message: Set<ChatSocketListener<"message">>;
  }>({
    open: new Set(),
    close: new Set(),
    error: new Set(),
    message: new Set(),
  });

  // Stable subscribe API — same identity across renders so consumers can
  // pass it to dependency arrays without re-attaching every render.
  const socketRef = useRef<ChatSocket>({
    subscribe: <E extends ChatSocketEventName>(
      event: E,
      fn: ChatSocketListener<E>,
    ): (() => void) => {
      // Each branch narrows fn against the corresponding Set; the cast is
      // safe because event and fn are correlated by the generic E.
      const set = listenersRef.current[event] as Set<ChatSocketListener<E>>;
      set.add(fn);
      return (): void => {
        set.delete(fn);
      };
    },
  });

  useEffect(() => {
    if (channelId === null) {
      setConnection("idle");
      setError(null);
      return;
    }
    let cancelled = false;
    setConnection("connecting");
    setError(null);

    const ws = new WebSocketClient({
      http: getClient().http,
      channelId,
      backoffMs: BACKOFF_MS,
    });

    ws.on("open", () => {
      if (cancelled) return;
      setConnection("open");
      for (const fn of listenersRef.current.open) fn(undefined);
    });
    // close + error both signal the api-client is about to retry; surface
    // "reconnecting" rather than "closed" so the badge doesn't flicker
    // through an intermediate state on a fast reconnect.
    ws.on("close", (ev) => {
      if (cancelled) return;
      setConnection("reconnecting");
      for (const fn of listenersRef.current.close) fn(ev);
    });
    ws.on("error", (ev) => {
      if (cancelled) return;
      setConnection("reconnecting");
      for (const fn of listenersRef.current.error) fn(ev);
    });
    ws.on("message", (ev) => {
      if (cancelled) return;
      for (const fn of listenersRef.current.message) fn(ev);
    });

    /* eslint-disable @typescript-eslint/no-unnecessary-condition --
       `cancelled` is mutated by the effect-cleanup closure; eslint
       can't see the cross-closure write across the await below. */
    void (async () => {
      try {
        await ws.connect();
      } catch (err) {
        if (cancelled) return;
        const msg = bannerMessage("Message connection failed", err);
        setError(msg);
        reportAppError(msg);
        setConnection("reconnecting");
      }
    })();
    /* eslint-enable @typescript-eslint/no-unnecessary-condition */

    return () => {
      cancelled = true;
      ws.close();
      setConnection("closed");
    };
  }, [channelId]);

  return {
    connection,
    error,
    socket: socketRef.current,
  };
}
