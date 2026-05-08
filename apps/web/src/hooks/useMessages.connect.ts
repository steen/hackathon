import { type Dispatch, type SetStateAction } from "react";
import { type Event as WsEvent, type Message } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";
import type { ChatSocket } from "./useChatSocket.js";
import {
  CATCHUP_LIMIT,
  type MessageView,
  mergeFetchedCatchup,
  type PendingMeta,
  RECONCILE_WINDOW_MS,
  reconcileWsMessage,
} from "./useMessages.helpers.js";

// Channel-bootstrap glue extracted from the useMessages effect. Owns:
// initial history fetch, history-error banner, reconnect-catchup, and
// wiring message-frame reconciliation onto the shared ChatSocket
// subscriptions. The WebSocket lifecycle itself (ticket fetch,
// reconnect cadence, connection-state surface) lives in useChatSocket;
// this module just attaches typed listeners to it.

export interface CancelToken {
  cancelled: boolean;
}

export interface ChannelDeps {
  channelId: string;
  tok: CancelToken;
  socket: ChatSocket;
  setMessages: Dispatch<SetStateAction<MessageView[]>>;
  setError: (s: string | null) => void;
  setCanLoadOlder: (v: boolean) => void;
  setHistoryLoading: (v: boolean) => void;
  pendingMetaRef: { current: Map<string, PendingMeta> };
  userIdRef: { current: string | null };
}

/**
 * Wires history fetch + WS subscriptions for one channel. Returns a
 * teardown function that detaches the subscriptions. Listeners attach
 * synchronously so a fast WS open can't slip past before our handlers
 * land; the history fetch runs concurrently.
 */
export function connectChannel(d: ChannelDeps): () => void {
  const { channelId, tok } = d;

  // Catchup-on-reopen counts opens locally so the first one (initial
  // connect) skips the refetch — initial history covers it.
  let openCount = 0;
  const unsubscribers: (() => void)[] = [];
  unsubscribers.push(
    d.socket.subscribe("open", () => {
      if (tok.cancelled) return;
      openCount += 1;
      if (openCount > 1) {
        void (async () => {
          try {
            const recent = await getClient().listMessages(channelId, { limit: CATCHUP_LIMIT });
            if (tok.cancelled) return;
            d.setMessages((prev) => mergeFetchedCatchup(prev, recent) ?? prev);
          } catch {
            /* swallow: catchup failure is non-fatal; banner stays clean. */
          }
        })();
      }
    }),
  );
  unsubscribers.push(
    d.socket.subscribe("message", (ev: WsEvent) => {
      if (tok.cancelled || ev.type !== "message") return;
      d.setMessages((prev) => {
        const result = reconcileWsMessage(
          prev,
          ev.data as Message,
          d.userIdRef.current,
          d.pendingMetaRef.current,
          RECONCILE_WINDOW_MS,
        );
        if (result.matchedPendingId !== null) {
          d.pendingMetaRef.current.delete(result.matchedPendingId);
        }
        return result.next;
      });
    }),
  );

  void (async () => {
    try {
      const history = await getClient().listMessages(channelId, { limit: CATCHUP_LIMIT });
      if (tok.cancelled) return;
      // Server returns newest-first; reverse so state is oldest→newest.
      d.setMessages([...history].reverse());
      // Full page → more older history might exist; short page → start in view.
      d.setCanLoadOlder(history.length >= CATCHUP_LIMIT);
    } catch (err) {
      if (tok.cancelled) return;
      const msg = bannerMessage("Failed to load message history", err);
      d.setError(msg);
      reportAppError(msg);
    } finally {
      if (!tok.cancelled) d.setHistoryLoading(false);
    }
  })();

  return () => {
    for (const off of unsubscribers) off();
  };
}
