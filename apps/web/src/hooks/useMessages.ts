import { useCallback, useEffect, useRef, useState } from "react";
import { type Message } from "@hackathon/api-client";
import type { ConnectionStatus } from "@hackathon/chat-ui";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError, userFacingMessage } from "../lib/userFacingError.js";
import { type CancelToken, connectChannel } from "./useMessages.connect.js";
import { BACKOFF_MS, useChatSocket } from "./useChatSocket.js";
import {
  CATCHUP_LIMIT,
  makePendingRow,
  markPendingFailed,
  type MessageView,
  newPendingId,
  oldestCommittedId,
  type PendingMeta,
  prependOlderPage,
  reconcilePersisted,
  startRetry,
} from "./useMessages.helpers.js";

export type { MessageView } from "./useMessages.helpers.js";
export { BACKOFF_MS };

interface UseMessages {
  messages: MessageView[];
  connection: ConnectionStatus;
  error: string | null;
  /** True from mount until the initial listMessages fetch settles. */
  historyLoading: boolean;
  send: (body: string) => Promise<void>;
  retry: (pendingId: string) => Promise<void>;
  loadOlder: () => Promise<void>;
  canLoadOlder: boolean;
  /** True while a loadOlder fetch is in flight. */
  isLoadingOlder: boolean;
  /** Curated banner for the latest loadOlder failure; cleared on next attempt. */
  loadOlderError: string | null;
}

export function useMessages(channelId: string | null, currentUserId?: string | null): UseMessages {
  const [messages, setMessages] = useState<MessageView[]>([]);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [canLoadOlder, setCanLoadOlder] = useState<boolean>(false);
  const [isLoadingOlder, setIsLoadingOlder] = useState<boolean>(false);
  const [loadOlderError, setLoadOlderError] = useState<string | null>(null);
  const [historyLoading, setHistoryLoading] = useState<boolean>(false);
  const loadingOlderRef = useRef<boolean>(false);
  const messagesRef = useRef<MessageView[]>([]);
  // Per-pendingId submit timestamps drive the WS-echo reconcile window
  // without leaking timestamps into MessageView rows.
  const pendingMetaRef = useRef<Map<string, PendingMeta>>(new Map());
  const userIdRef = useRef<string | null>(currentUserId ?? null);
  const channelIdRef = useRef<string | null>(channelId);
  userIdRef.current = currentUserId ?? null;
  channelIdRef.current = channelId;
  useEffect(() => {
    messagesRef.current = messages;
  }, [messages]);

  const { connection, error: socketError, socket } = useChatSocket(channelId);

  useEffect(() => {
    setMessages([]);
    setCanLoadOlder(false);
    setIsLoadingOlder(false);
    setLoadOlderError(null);
    pendingMetaRef.current.clear();
    if (channelId === null) {
      setHistoryLoading(false);
      return;
    }
    const tok: CancelToken = { cancelled: false };
    setHistoryError(null);
    setHistoryLoading(true);
    loadingOlderRef.current = false;

    const detach = connectChannel({
      channelId,
      tok,
      socket,
      setMessages,
      setError: setHistoryError,
      setCanLoadOlder,
      setHistoryLoading,
      pendingMetaRef,
      userIdRef,
    });

    return () => {
      tok.cancelled = true;
      detach();
    };
  }, [channelId, socket]);

  const submitPending = useCallback(async (id: string, ch: string, body: string): Promise<void> => {
    pendingMetaRef.current.set(id, { submittedAt: Date.now() });
    try {
      const persisted = (await getClient().postMessage(ch, body)) as Message | undefined;
      // Reconcile from REST when present; the WS echo with the same id
      // is dropped by reconcileWsMessage's dedup. `persisted` is
      // undefined under test fixtures mocking postMessage as void —
      // leave the optimistic row to reconcile via the WS echo.
      if (persisted !== undefined) {
        pendingMetaRef.current.delete(id);
        setMessages((prev) => reconcilePersisted(prev, id, persisted));
      }
    } catch (err) {
      const reason = userFacingMessage("Failed to send message", err);
      setMessages((prev) => markPendingFailed(prev, id, reason));
    }
  }, []);

  const send = useCallback(
    async (body: string): Promise<void> => {
      const ch = channelIdRef.current;
      if (ch === null) return;
      const trimmed = body.trim();
      if (trimmed.length === 0) return;
      const id = newPendingId();
      const synthetic = makePendingRow(id, ch, userIdRef.current ?? "", trimmed);
      setMessages((prev) => [...prev, synthetic]);
      setHistoryError(null);
      await submitPending(id, ch, trimmed);
    },
    [submitPending],
  );

  const retry = useCallback(
    async (pendingId: string): Promise<void> => {
      const ch = channelIdRef.current;
      if (ch === null) return;
      const captured: { body: string | undefined } = { body: undefined };
      setMessages((prev) => {
        const { next, body } = startRetry(prev, pendingId);
        captured.body = body;
        return next;
      });
      if (captured.body === undefined) return;
      setHistoryError(null);
      await submitPending(pendingId, ch, captured.body);
    },
    [submitPending],
  );

  const loadOlder = useCallback(async (): Promise<void> => {
    const ch = channelIdRef.current;
    if (ch === null) return;
    if (loadingOlderRef.current) return;
    const oldestId = oldestCommittedId(messagesRef.current);
    if (oldestId === undefined) return;
    loadingOlderRef.current = true;
    setIsLoadingOlder(true);
    setLoadOlderError(null);
    try {
      const page = await getClient().listMessages(ch, { before: oldestId, limit: CATCHUP_LIMIT });
      // Dedup against the ref outside the reducer: under StrictMode the
      // reducer runs twice, and a stashed count would record the second
      // pass's view. WS frames between fetch and here only append newer
      // rows, so older-window dedup against the ref is correct.
      const refSeen = new Set(messagesRef.current.map((p) => p.id));
      const refFreshCount = page.filter((m) => !refSeen.has(m.id)).length;
      setMessages((prev) => prependOlderPage(prev, page) ?? prev);
      // Gate on deduped fresh count, not raw page size: a mostly-
      // overlapping full page means the channel's start is in view (#589).
      setCanLoadOlder(refFreshCount >= CATCHUP_LIMIT);
    } catch (err) {
      const msg = bannerMessage("Failed to load older messages", err);
      setLoadOlderError(msg);
      reportAppError(msg);
    } finally {
      loadingOlderRef.current = false;
      setIsLoadingOlder(false);
    }
  }, []);

  // History-error and socket-error share one banner slot to the consumer.
  // historyError takes priority because it indicates a missing initial
  // page (user sees no rows); socket failures surface only when no
  // history banner is up.
  const error = historyError ?? socketError;

  return {
    messages,
    connection,
    error,
    historyLoading,
    send,
    retry,
    loadOlder,
    canLoadOlder,
    isLoadingOlder,
    loadOlderError,
  };
}
