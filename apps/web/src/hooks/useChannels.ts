import { useCallback, useEffect, useState } from "react";
import type { Channel, Event as WsEvent, ChannelEvent } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";
import type { ChatSocket } from "./useChatSocket.js";

// Narrow the WS event union against ChannelEvent. The `Event` union
// includes `UnknownEvent` (type: string), so `ev.type === "channel"`
// alone leaves `ev.data` as `unknown`; this guard isolates the safe
// projection so a future field rename in ChannelEvent.data fails type
// checking here instead of slipping past a hand-written cast.
function isChannelEvent(ev: WsEvent): ev is ChannelEvent {
  return ev.type === "channel";
}

interface ChannelsState {
  channels: Channel[];
  loading: boolean;
  error: string | null;
}

interface UseChannels extends ChannelsState {
  reload: () => Promise<void>;
  create: (name: string) => Promise<Channel>;
  rename: (id: string, newName: string) => Promise<Channel>;
}

interface UseChannelsOpts {
  /** Shared chat socket. When provided, the hook subscribes to `channel`
   *  events for live merge-by-id and triggers a reload() on every WS open
   *  (initial connect + reconnect) as a catch-up against missed frames
   *  while the channel snapshot was idle. */
  socket?: ChatSocket;
}

export function useChannels(enabled: boolean, opts: UseChannelsOpts = {}): UseChannels {
  const { socket } = opts;
  const [state, setState] = useState<ChannelsState>({
    channels: [],
    loading: false,
    error: null,
  });

  const reload = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      const list = await getClient().listChannels();
      setState({ channels: list, loading: false, error: null });
    } catch (err) {
      const msg = bannerMessage("Failed to load channels", err);
      setState((s) => ({ ...s, loading: false, error: msg }));
      reportAppError(msg);
    }
  }, []);

  // Eager merge on REST success so the caller can switch to the new id
  // before the WS frame arrives; the WS handler's id-dedup makes the
  // echo a no-op.
  const create = useCallback(async (name: string): Promise<Channel> => {
    const ch = await getClient().createChannel(name);
    setState((s) =>
      s.channels.some((c) => c.id === ch.id) ? s : { ...s, channels: [...s.channels, ch] },
    );
    return ch;
  }, []);

  const rename = useCallback(async (id: string, newName: string): Promise<Channel> => {
    const ch = await getClient().renameChannel(id, newName);
    setState((s) => ({
      ...s,
      channels: s.channels.map((c) => (c.id === ch.id ? { ...c, name: ch.name } : c)),
    }));
    return ch;
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void reload();
  }, [enabled, reload]);

  // Live channel-event merge + reload-on-open catchup.
  useEffect(() => {
    if (!enabled || socket === undefined) return;
    const offMessage = socket.subscribe("message", (ev) => {
      if (!isChannelEvent(ev)) return;
      const next = ev.data.channel;
      setState((s) => {
        const existing = s.channels.find((c) => c.id === next.id);
        if (ev.data.kind === "create") {
          if (existing !== undefined) return s;
          return { ...s, channels: [...s.channels, next] };
        }
        // rename — upsert if we've never heard of the id (lost catchup window).
        if (existing === undefined) {
          return { ...s, channels: [...s.channels, next] };
        }
        if (existing.name === next.name) return s;
        return {
          ...s,
          channels: s.channels.map((c) => (c.id === next.id ? { ...c, name: next.name } : c)),
        };
      });
    });
    // Cross-tab/device sync: a `{type:"read", scope:"channel"}` frame is
    // routed to the originating viewer's user:<viewer> topic on every
    // POST /channels/{id}/read commit (server emits after UPSERT — see
    // specs/plans/phase-9/read-state.md). When tab A advances, tab B's
    // socket receives this frame; we overwrite the local unread_count
    // for the matching channel id per decision-log §12 (reconcile-
    // overwrite, never max). last_read_message_id is also kept in sync
    // so a subsequent listing fetch doesn't see a stale baseline.
    const offRead = socket.subscribe("read", (ev) => {
      if (ev.data.scope !== "channel") return;
      const targetId = ev.data.target_id;
      const lastReadId = ev.data.last_read_message_id;
      const unread = ev.data.unread_count;
      setState((s) => {
        if (!s.channels.some((c) => c.id === targetId)) return s;
        return {
          ...s,
          channels: s.channels.map((c) =>
            c.id === targetId
              ? { ...c, unread_count: unread, last_read_message_id: lastReadId }
              : c,
          ),
        };
      });
    });
    const offOpen = socket.subscribe("open", () => {
      void reload();
    });
    return () => {
      offMessage();
      offRead();
      offOpen();
    };
  }, [enabled, socket, reload]);

  return { ...state, reload, create, rename };
}
