import { type Dispatch, type SetStateAction } from "react";
import { WebSocketClient, type Event as WsEvent, type Message } from "@hackathon/api-client";
import type { ConnectionStatus } from "@hackathon/chat-ui";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";
import {
  BACKOFF_MS,
  CATCHUP_LIMIT,
  type MessageView,
  mergeFetchedCatchup,
  type PendingMeta,
  RECONCILE_WINDOW_MS,
  reconcileWsMessage,
} from "./useMessages.helpers.js";

// Impure channel-bootstrap function extracted from the useMessages
// effect. Owns: initial history fetch, WebSocket construction + handler
// wiring, reconnect-catchup, error banners. Kept out of useMessages.ts
// so the hook reads as pure state-wiring + four short callbacks; kept
// out of useMessages.helpers.ts because it touches network and refs.

export interface CancelToken {
  cancelled: boolean;
}

export interface ChannelDeps {
  channelId: string;
  tok: CancelToken;
  setMessages: Dispatch<SetStateAction<MessageView[]>>;
  setConnection: (s: ConnectionStatus) => void;
  setError: (s: string | null) => void;
  setCanLoadOlder: (v: boolean) => void;
  setHistoryLoading: (v: boolean) => void;
  wsRef: { current: WebSocketClient | null };
  pendingMetaRef: { current: Map<string, PendingMeta> };
  userIdRef: { current: string | null };
}

export async function connectChannel(d: ChannelDeps): Promise<void> {
  const { channelId, tok } = d;
  /* eslint-disable @typescript-eslint/no-unnecessary-condition --
     tok.cancelled is mutated by the caller's effect-cleanup closure;
     eslint can't see the cross-closure write and flags every check as
     always-falsy. */
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
  if (tok.cancelled) return;

  const ws = new WebSocketClient({ http: getClient().http, channelId, backoffMs: BACKOFF_MS });
  d.wsRef.current = ws;
  let openCount = 0;
  const onReconnecting = (): void => {
    if (!tok.cancelled) d.setConnection("reconnecting");
  };
  ws.on("open", () => {
    if (tok.cancelled) return;
    d.setConnection("open");
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
  });
  ws.on("close", onReconnecting);
  ws.on("error", onReconnecting);
  ws.on("message", (ev: WsEvent) => {
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
  });
  try {
    await ws.connect();
  } catch (err) {
    if (tok.cancelled) return;
    const msg = bannerMessage("Message connection failed", err);
    d.setError(msg);
    reportAppError(msg);
    d.setConnection("reconnecting");
  }
  /* eslint-enable @typescript-eslint/no-unnecessary-condition */
}
