import { useCallback, useEffect, useState } from "react";
import type { Channel } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";
import type { ChatSocket } from "./useChatSocket.js";

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
      if (ev.type !== "channel") return;
      const data = ev.data as { kind: "create" | "rename"; channel: Channel };
      const next = data.channel;
      setState((s) => {
        const existing = s.channels.find((c) => c.id === next.id);
        if (data.kind === "create") {
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
    const offOpen = socket.subscribe("open", () => {
      void reload();
    });
    return () => {
      offMessage();
      offOpen();
    };
  }, [enabled, socket, reload]);

  return { ...state, reload, create, rename };
}
