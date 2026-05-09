import { useCallback, useEffect, useState } from "react";
import {
  createDM,
  listDMs,
  type Conversation,
  type DMEvent,
  type ReadEvent,
} from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";
import type { ChatSocket } from "./useChatSocket.js";

// Owner of the per-tab DM conversation list + unread bookkeeping.
//
// Lifecycle:
//   1. On mount (when enabled), fetches GET /api/dms and seeds the list,
//      sorted by last_message_at DESC. Empty conversations are already
//      hidden by the server (decision §3 — listing query filters
//      last_message_id IS NOT NULL).
//   2. Subscribes to the shared chat socket's typed `dm` slot. Every
//      incoming {type:"dm"} frame:
//        - upserts/refreshes the conversation row from the embedded
//          `conversation` block (decision §8 self-sufficiency — the
//          frame stands alone on first contact).
//        - increments local unread_count by 1 when the sender is NOT
//          the viewer (decision §12 increment rule).
//   3. Subscribes to `read` frames with scope==="dm" and zeroes the
//      matching conversation's unread_count for cross-tab/device sync
//      (decision §7 + §12).
//   4. On every WS open (initial + reconnect) calls reload() so any
//      frames missed during the gap are reconciled — and reload()
//      OVERWRITES local unread counters with the server's authoritative
//      value (decision §12 reconcile-overwrite).

interface DMState {
  conversations: Conversation[];
  loading: boolean;
  error: string | null;
}

export interface UseDMs extends DMState {
  reload: () => Promise<void>;
  /** Idempotent find-or-create. Returns the (possibly empty) conversation. */
  startWith: (peerUserId: string) => Promise<Conversation>;
}

interface UseDMsOpts {
  /** Self user id — used to skip incrementing unread for the viewer's own
   *  sends (server fans the {type:"dm"} frame to BOTH participants). */
  selfUserId: string | null;
  /** Shared chat socket. When provided, the hook subscribes to dm + read
   *  frames and triggers reload() on every WS open as gap-recovery. */
  socket?: ChatSocket;
}

function sortByActivity(rows: Conversation[]): Conversation[] {
  // last_message_at can be null on a freshly-created conversation that
  // hasn't seen a message yet (e.g. immediately after POST /api/dms). The
  // listing endpoint filters those out per §3, so they only appear via
  // the local startWith() optimistic add. Push null-tipped rows last so
  // they don't claim the top slot until the first message lands.
  return [...rows].sort((a, b) => {
    const aT = a.last_message_at;
    const bT = b.last_message_at;
    if (aT === null && bT === null) return 0;
    if (aT === null) return 1;
    if (bT === null) return -1;
    if (aT === bT) return 0;
    return aT < bT ? 1 : -1;
  });
}

export function useDMs(enabled: boolean, opts: UseDMsOpts): UseDMs {
  const { selfUserId, socket } = opts;
  const [state, setState] = useState<DMState>({
    conversations: [],
    loading: false,
    error: null,
  });

  const reload = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      // The server marshals an empty conversations slice as JSON `null`
      // when there are no rows (Go nil slice), so listDMs may resolve to
      // a value the typed signature doesn't admit. Cast through unknown
      // to a nullable shape, then default to []. The TS contract
      // (api-client/dms.ts) stays strict for non-empty paths.
      const raw = (await listDMs(getClient().http)) as Conversation[] | null;
      const rows = raw ?? [];
      setState({ conversations: sortByActivity(rows), loading: false, error: null });
    } catch (err) {
      const msg = bannerMessage("Failed to load direct messages", err);
      setState((s) => ({ ...s, loading: false, error: msg }));
      reportAppError(msg);
    }
  }, []);

  // Idempotent find-or-create. The conversation may have no messages — we
  // still merge it into local state so the consumer can navigate to a
  // freshly-created (empty) thread. The listing query will hide it on
  // next reload until a message is sent (§3); that's fine — the local
  // entry stays put as long as the user has the thread open.
  const startWith = useCallback(async (peerUserId: string): Promise<Conversation> => {
    const conv = await createDM(getClient().http, peerUserId);
    setState((s) => {
      const existing = s.conversations.findIndex((c) => c.id === conv.id);
      const next =
        existing >= 0
          ? s.conversations.map((c, i) => (i === existing ? conv : c))
          : [...s.conversations, conv];
      return { ...s, conversations: sortByActivity(next) };
    });
    return conv;
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void reload();
  }, [enabled, reload]);

  useEffect(() => {
    if (!enabled || socket === undefined) return;

    // {type:"dm"} — upsert the conversation, increment unread for non-self
    // senders. The frame's `conversation.unread_count` is the server's
    // baseline at frame-emit time; we overlay our local increment on top
    // since the server doesn't know which client tab has the thread open.
    const offDM = socket.subscribe("dm", (ev: DMEvent) => {
      const incoming = ev.data.conversation;
      const senderId = ev.data.dm_message.sender_user_id;
      const isSelf = selfUserId !== null && senderId === selfUserId;
      setState((s) => {
        const idx = s.conversations.findIndex((c) => c.id === incoming.id);
        if (idx < 0) {
          // First-DM case: §8 self-sufficient frame seeds the sidebar
          // entry. The server-supplied unread_count IS the count after
          // this message (peer→viewer means it's already 1 for non-self;
          // for the sender's echo on a fresh conversation it's 0).
          return { ...s, conversations: sortByActivity([...s.conversations, incoming]) };
        }
        const next = s.conversations.slice();
        const prev = next[idx];
        if (prev === undefined) return s;
        // Refresh wire fields from the embedded block, then layer the
        // local increment over the server-supplied unread_count when the
        // sender is the peer (the server's baseline is "as of frame
        // emit", but local already saw earlier frames in the same WS
        // session that the listing fetch hasn't reconciled yet).
        const nextUnread = isSelf
          ? incoming.unread_count
          : Math.max(incoming.unread_count, prev.unread_count + 1);
        next[idx] = {
          ...incoming,
          unread_count: nextUnread,
        };
        return { ...s, conversations: sortByActivity(next) };
      });
    });

    // {type:"read", scope:"dm"} — cross-tab/device sync. The originating
    // tab's POST /read advance is mirrored to the user:<viewer> topic so
    // every other tab zeroes its local badge without a refetch.
    const offRead = socket.subscribe("read", (ev: ReadEvent) => {
      if (ev.data.scope !== "dm") return;
      const targetId = ev.data.target_id;
      const unread = ev.data.unread_count;
      setState((s) => {
        const idx = s.conversations.findIndex((c) => c.id === targetId);
        if (idx < 0) return s;
        const next = s.conversations.slice();
        const prev = next[idx];
        if (prev === undefined) return s;
        if (prev.unread_count === unread) return s;
        next[idx] = { ...prev, unread_count: unread };
        return { ...s, conversations: next };
      });
    });

    // Reconnect catchup: any frames missed while the socket was down are
    // recovered by reloading the listing — which OVERWRITES local
    // unread_count per §12 reconcile rule.
    const offOpen = socket.subscribe("open", () => {
      void reload();
    });

    return () => {
      offDM();
      offRead();
      offOpen();
    };
  }, [enabled, socket, selfUserId, reload]);

  return { ...state, reload, startWith };
}
