import { useEffect, useState } from "react";
import { WebSocketClient, type Event as WsEvent } from "@hackathon/api-client";
import { getClient } from "../api.js";

export interface PresenceUser {
  id: string;
  username: string;
}

export interface UsePresence {
  users: PresenceUser[];
  loading: boolean;
  error: string | null;
}

type PresenceState = UsePresence;

interface PresenceListResponse {
  users: PresenceUser[];
}

interface PresenceFrameData {
  kind: "join" | "leave";
  user_id: string;
}

const BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000];

interface CancelToken {
  cancelled: boolean;
}

function sortUsers(users: PresenceUser[]): PresenceUser[] {
  // Server `presence` WS frames omit username, so a user added from a
  // live join carries an empty string until the next page load reseeds
  // from /api/presence. Sort named entries first so the list reads
  // alphabetically by username, with anonymous (id-only) entries
  // pushed to the bottom and tie-broken by id.
  return [...users].sort((a, b) => {
    const aNamed = a.username.length > 0;
    const bNamed = b.username.length > 0;
    if (aNamed !== bNamed) return aNamed ? -1 : 1;
    if (a.username !== b.username) return a.username < b.username ? -1 : 1;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}

export function usePresence(enabled: boolean): UsePresence {
  const [state, setState] = useState<PresenceState>({
    users: [],
    loading: false,
    error: null,
  });

  useEffect(() => {
    if (!enabled) {
      setState({ users: [], loading: false, error: null });
      return;
    }

    const tok: CancelToken = { cancelled: false };
    let ws: WebSocketClient | null = null;

    setState((s) => ({ ...s, loading: true, error: null }));

    /* eslint-disable @typescript-eslint/no-unnecessary-condition --
       tok.cancelled is mutated by the cleanup closure; eslint's flow
       analysis cannot see the cross-closure write so flags every check
       as always-falsy. */
    void (async () => {
      try {
        const seed = await getClient().http.request<PresenceListResponse>("GET", "/api/presence");
        if (tok.cancelled) return;
        setState({ users: sortUsers(seed.users), loading: false, error: null });
      } catch (err) {
        if (tok.cancelled) return;
        const msg = err instanceof Error ? err.message : "failed to load presence";
        setState((s) => ({ ...s, loading: false, error: msg }));
      }

      if (tok.cancelled) return;

      ws = new WebSocketClient({
        http: getClient().http,
        backoffMs: BACKOFF_MS,
      });
      ws.on("message", (ev: WsEvent) => {
        if (tok.cancelled) return;
        if (ev.type !== "presence") return;
        const data = ev.data as PresenceFrameData;
        if (typeof data.user_id !== "string" || data.user_id.length === 0) return;
        setState((prev) => {
          if (data.kind === "join") {
            if (prev.users.some((u) => u.id === data.user_id)) return prev;
            return {
              ...prev,
              users: sortUsers([...prev.users, { id: data.user_id, username: "" }]),
            };
          }
          if (data.kind === "leave") {
            const next = prev.users.filter((u) => u.id !== data.user_id);
            if (next.length === prev.users.length) return prev;
            return { ...prev, users: next };
          }
          return prev;
        });
      });
      try {
        await ws.connect();
      } catch (err) {
        if (tok.cancelled) return;
        const msg = err instanceof Error ? err.message : "presence websocket failed";
        setState((s) => ({ ...s, error: msg }));
      }
    })();
    /* eslint-enable @typescript-eslint/no-unnecessary-condition */

    return () => {
      tok.cancelled = true;
      if (ws !== null) {
        ws.close();
        ws = null;
      }
    };
  }, [enabled]);

  return state;
}
