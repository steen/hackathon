import { useCallback, useEffect, useState } from "react";
import type { Channel } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage } from "../lib/userFacingError.js";

interface ChannelsState {
  channels: Channel[];
  loading: boolean;
  error: string | null;
}

interface UseChannels extends ChannelsState {
  reload: () => Promise<void>;
  create: (name: string) => Promise<Channel>;
}

export function useChannels(enabled: boolean): UseChannels {
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
    }
  }, []);

  const create = useCallback(
    async (name: string): Promise<Channel> => {
      const ch = await getClient().createChannel(name);
      await reload();
      return ch;
    },
    [reload],
  );

  useEffect(() => {
    if (!enabled) return;
    void reload();
  }, [enabled, reload]);

  return { ...state, reload, create };
}
