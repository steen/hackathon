import type * as React from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  listDMMessages,
  sendDMMessage,
  type Conversation,
  type DMEvent,
  type DMMessage,
} from "@hackathon/api-client";
import {
  MESSAGE_MAX_BYTES,
  MessageComposer,
  MessageList,
  type ChatMessage,
} from "@hackathon/chat-ui";
import { getClient } from "../api.js";
import type { ChatSocket } from "../hooks/useChatSocket.js";
import { useReadMarker } from "../hooks/useReadMarker.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";

// Active-thread renderer for a DM conversation. Mounts once per (active)
// conversation id and:
//   1. Loads the most recent page of messages (newest-first wire shape;
//      reversed for display) via GET /api/dms/{id}/messages.
//   2. Subscribes to {type:"dm"} frames on the shared chat socket and
//      appends frames whose conversation_id matches the active thread.
//      The sidebar's useDMs hook handles list-level updates; this
//      component only renders the thread itself.
//   3. Wires `useReadMarker("dm", conversationId)` so the latest visible
//      message id advances the server-side read pointer (debounced 250ms,
//      flushed on focus return / unmount per useReadMarker.ts).
//   4. Sends new messages via POST /api/dms/{id}/messages; the WS echo
//      arrives on the same socket and is folded back into the list. The
//      composer mirrors the channel composer behavior (Enter to send,
//      4096-byte cap shared with channel messages — L16).

interface Props {
  conversation: Conversation;
  selfUserId: string | null;
  resolveSender: (id: string) => string;
  socket: ChatSocket;
}

const HISTORY_LIMIT = 50;

export function DMThread(props: Props): React.JSX.Element {
  const { conversation, selfUserId, resolveSender, socket } = props;
  const [messages, setMessages] = useState<DMMessage[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  const conversationId = conversation.id;
  const marker = useReadMarker("dm", conversationId);
  const markRead = marker.markRead;

  // Load history on conversation change. The wire shape from
  // listDMMessages is newest-first; reverse so the rendered list stays
  // chronological top-to-bottom (matching channel messages).
  useEffect(() => {
    let cancelled = false;
    setHistoryLoading(true);
    setHistoryError(null);
    setMessages([]);
    /* eslint-disable @typescript-eslint/no-unnecessary-condition --
       `cancelled` is mutated by the effect-cleanup closure; eslint
       can't see the cross-closure write across the await below. */
    void (async () => {
      try {
        const page = await listDMMessages(getClient().http, conversationId, {
          limit: HISTORY_LIMIT,
        });
        if (cancelled) return;
        // The endpoint returns newest-first (mirrors channel pagination).
        // Reverse for display so the most recent row sits at the bottom.
        const chrono = [...page].reverse();
        setMessages(chrono);
        setHistoryLoading(false);
      } catch (err) {
        if (cancelled) return;
        const msg = bannerMessage("Failed to load direct messages", err);
        setHistoryError(msg);
        setHistoryLoading(false);
        reportAppError(msg);
      }
    })();
    /* eslint-enable @typescript-eslint/no-unnecessary-condition */
    return () => {
      cancelled = true;
    };
  }, [conversationId]);

  // Live-append incoming dm frames matching this conversation. Dedup by
  // id so a freshly-sent message that round-trips via REST + WS only
  // renders once; the REST response is appended optimistically below.
  useEffect(() => {
    const off = socket.subscribe("dm", (ev: DMEvent) => {
      const msg = ev.data.dm_message;
      if (msg.conversation_id !== conversationId) return;
      setMessages((prev) => {
        if (prev.some((m) => m.id === msg.id)) return prev;
        return [...prev, msg];
      });
    });
    return off;
  }, [socket, conversationId]);

  // Advance the read pointer whenever the latest committed message
  // changes — useReadMarker debounces (250ms trailing) and flushes on
  // visibility/focus return. Skip when the tab is hidden (the focus
  // return path handles the catchup) and when the latest message is the
  // viewer's own send (the sender's read row is advanced server-side
  // inside InsertDMMessageTx — decision §11).
  const latestPeerMessageId = (() => {
    for (let i = messages.length - 1; i >= 0; i -= 1) {
      const m = messages[i];
      if (m === undefined) continue;
      if (selfUserId !== null && m.sender_user_id === selfUserId) continue;
      return m.id;
    }
    return null;
  })();

  useEffect(() => {
    if (latestPeerMessageId === null) return;
    if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
    markRead(latestPeerMessageId);
  }, [latestPeerMessageId, markRead]);

  const submit = useCallback(async (): Promise<void> => {
    const body = draft.trim();
    if (body.length === 0) return;
    if (sending) return;
    setSending(true);
    setDraft("");
    try {
      const persisted = await sendDMMessage(getClient().http, conversationId, body);
      setMessages((prev) => {
        if (prev.some((m) => m.id === persisted.id)) return prev;
        return [...prev, persisted];
      });
    } catch (err) {
      // Restore the draft so the user can retry; surface a banner.
      setDraft(body);
      const msg = bannerMessage("Failed to send direct message", err);
      reportAppError(msg);
    } finally {
      setSending(false);
    }
  }, [draft, sending, conversationId]);

  // Lift focus to the composer when the thread mounts so keyboard users
  // land ready-to-type without an extra Tab. Re-run on conversationId
  // change so switching threads also focuses the new composer.
  useEffect(() => {
    composerRef.current?.focus();
  }, [conversationId]);

  // MessageList consumes a structural ChatMessage shape. DMMessage already
  // carries id/sender_user_id/body/created_at; map directly.
  const chatRows: ChatMessage[] = messages.map((m) => ({
    id: m.id,
    sender_user_id: m.sender_user_id,
    body: m.body,
    created_at: m.created_at,
  }));

  const peerName =
    conversation.peer.username.length > 0 ? conversation.peer.username : conversation.peer.id;

  return (
    <main className="messages" aria-label={`Direct message with ${peerName}`}>
      <div className="messages__header-row">
        <h2 className="channel-header" tabIndex={-1}>
          {peerName}
        </h2>
      </div>
      <MessageList
        messages={chatRows}
        resolveSender={resolveSender}
        selfUserId={selfUserId}
        error={historyError}
        showEmptyChannelHint={!historyLoading && historyError === null && messages.length === 0}
        emptyChannelHintText={`This is the start of your conversation with ${peerName} — send a message to say hi.`}
        listRef={listRef}
      />
      <MessageComposer
        value={draft}
        onChange={setDraft}
        onSubmit={() => {
          void submit();
        }}
        disabled={sending}
        maxBytes={MESSAGE_MAX_BYTES}
        placeholder={`Message ${peerName}`}
        composerRef={composerRef}
      />
    </main>
  );
}
