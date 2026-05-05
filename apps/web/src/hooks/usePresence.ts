import { useEffect, useState } from "react";
import { WebSocketClient, type Event as WsEvent } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";

export interface PresenceUser {
  id: string;
  username: string;
}

export interface PresenceEvent {
  kind: "join" | "leave";
  id: string;
  // Empty string means the username was not in the seeded directory at the
  // time the event arrived. The Chat live region renders a generic phrase
  // in that case rather than reading out the raw UUID.
  username: string;
  // Monotonic per-mount counter so repeat join/leave for the same id still
  // re-fires the live-region announcement (React skips updates when the
  // value is referentially equal).
  seq: number;
}

export interface UsePresence {
  users: PresenceUser[];
  // Sticky username directory keyed by user id. Accumulates every username
  // the client has seen this session (seed + any future reseed) so message
  // rows can resolve a sender id even after the user has left the channel.
  // Never shrinks — leaving the room doesn't erase the entry.
  usernames: Map<string, string>;
  loading: boolean;
  error: string | null;
  lastEvent: PresenceEvent | null;
}

type PresenceState = UsePresence;

interface PresenceListResponse {
  users: PresenceUser[];
}

interface PresenceFrameData {
  kind: "join" | "leave";
  user_id: string;
  // Server-populated username for the affected user (#490). The field is
  // additive — older servers omit it, in which case we fall back to the
  // seeded `knownUsernames` directory below.
  username?: string;
}

const BACKOFF_MS = [500, 1000, 2000, 5000, 10000, 20000, 30000];

interface CancelToken {
  cancelled: boolean;
}

// Reference-stability helper for the exposed username directory. Consumers
// (Chat's resolveSender) wrap the map in `useCallback` keyed on its identity;
// re-creating the map on every seed/reseed when the (id → username) pairs
// haven't changed would defeat that memoization. Compare entry-by-entry so we
// can keep the previous reference when content is unchanged. #566.
function sameUsernames(a: Map<string, string>, b: Map<string, string>): boolean {
  if (a === b) return true;
  if (a.size !== b.size) return false;
  for (const [k, v] of a) {
    if (b.get(k) !== v) return false;
  }
  return true;
}

function sortUsers(users: PresenceUser[]): PresenceUser[] {
  // Server `presence` WS frames now carry username (#490) when the
  // wiring registers a resolver, but the field is still optional on
  // the wire. A user added from a live join can therefore carry an
  // empty string when the server omitted it. Sort named entries first
  // so the list reads alphabetically by username, with anonymous
  // (id-only) entries pushed to the bottom and tie-broken by id.
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
    usernames: new Map<string, string>(),
    loading: false,
    error: null,
    lastEvent: null,
  });

  useEffect(() => {
    if (!enabled) {
      setState((s) => ({
        users: [],
        // Preserve the existing reference when the directory is already
        // empty so a disabled→disabled remount doesn't churn consumer
        // memoization. #566.
        usernames: s.usernames.size === 0 ? s.usernames : new Map<string, string>(),
        loading: false,
        error: null,
        lastEvent: null,
      }));
      return;
    }

    const tok: CancelToken = { cancelled: false };
    let ws: WebSocketClient | null = null;
    // Directory of usernames the client has ever seen this session, keyed
    // by id. Seeded by /api/presence and accumulated on subsequent reseeds
    // (currently the hook only seeds once per mount, but a future periodic
    // reseed would feed this same map). Lets a join announcement render
    // the username for a user the SR-listener has heard of before, even if
    // they had previously left.
    //
    // Staleness window: the directory is populated exactly once per mount
    // and never refreshed. A user who registers after this hook mounted
    // will be unknown to the directory for the remaining lifetime of the
    // mount — their join is announced as a generic "a new user joined"
    // and their leave the same. Username changes (if/when supported) are
    // also not picked up. The bound is therefore "until the tab reloads
    // or the hook unmounts/remounts" rather than a fixed duration.
    // The directory still backstops frames that arrive without a
    // username (older servers, or any path where the server-side
    // resolver returned ""). #490 added the frame-carried username so
    // first-time-seen users no longer require a reseed for the live
    // region to read out their name; the staleness window above
    // therefore only bites users the server failed to resolve.
    const knownUsernames = new Map<string, string>();
    let seq = 0;

    setState((s) => ({ ...s, loading: true, error: null }));

    /* eslint-disable @typescript-eslint/no-unnecessary-condition --
       tok.cancelled is mutated by the cleanup closure; eslint's flow
       analysis cannot see the cross-closure write so flags every check
       as always-falsy. */
    void (async () => {
      try {
        const seed = await getClient().http.request<PresenceListResponse>("GET", "/api/presence");
        if (tok.cancelled) return;
        for (const u of seed.users) {
          if (u.username.length > 0) knownUsernames.set(u.id, u.username);
        }
        setState((s) => {
          // Snapshot the directory into state so consumers (Chat message
          // rows) re-render once the seed lands. Cloning here keeps the
          // closure-local map mutable without forcing every read through
          // a setState call. Reuse the prior reference when the (id →
          // username) pairs are unchanged so consumers that depend on
          // `usernames` identity (e.g. resolveSender's useCallback) skip
          // re-creation. #566.
          //
          // `s.usernames` here is the React state value, which lives above
          // this effect and survives effect cleanup. A disabled→enabled→
          // disabled→enabled cycle that re-seeds an identical directory
          // therefore preserves the reference — a property of state
          // placement, not a deliberate cache. #570.
          const next = new Map(knownUsernames);
          const usernames = sameUsernames(s.usernames, next) ? s.usernames : next;
          return {
            ...s,
            users: sortUsers(seed.users),
            usernames,
            loading: false,
            error: null,
          };
        });
      } catch (err) {
        if (tok.cancelled) return;
        const msg = bannerMessage("Failed to load presence", err);
        setState((s) => ({ ...s, loading: false, error: msg }));
        reportAppError(msg);
      }

      if (tok.cancelled) return;

      // Deliberate second WS per tab: the messages WS in useMessages
      // already receives `presence` frames via the hub's BroadcastAll,
      // but threading that subscription through React state requires
      // either a shared client in context or a presence event channel
      // on useMessages — both deferred. Server-side AddPresence is
      // ref-counted on userID, so two sockets from the same user still
      // fire only one join/leave pair.
      ws = new WebSocketClient({
        http: getClient().http,
        backoffMs: BACKOFF_MS,
      });
      ws.on("message", (ev: WsEvent) => {
        if (tok.cancelled) return;
        if (ev.type !== "presence") return;
        const data = ev.data as PresenceFrameData;
        if (typeof data.user_id !== "string" || data.user_id.length === 0) return;
        // Frame-carried username (#490) wins over the seeded directory
        // when present — it is authoritative for users who registered
        // after the hook mounted (the seed never refreshes). Empty string
        // means the server omitted the field (older server, or no lookup
        // registered) and we fall back to the directory.
        const frameUsername =
          typeof data.username === "string" && data.username.length > 0 ? data.username : "";
        if (frameUsername.length > 0) {
          knownUsernames.set(data.user_id, frameUsername);
        }
        setState((prev) => {
          // Mirror the frame-derived addition into exposed state so
          // consumers (Chat sender resolution) see new users picked up
          // from live joins, not just the initial seed.
          let usernames = prev.usernames;
          if (frameUsername.length > 0 && prev.usernames.get(data.user_id) !== frameUsername) {
            usernames = new Map(prev.usernames);
            usernames.set(data.user_id, frameUsername);
          }
          if (data.kind === "join") {
            const username =
              frameUsername.length > 0 ? frameUsername : (knownUsernames.get(data.user_id) ?? "");
            seq += 1;
            const event: PresenceEvent = {
              kind: "join",
              id: data.user_id,
              username,
              seq,
            };
            if (prev.users.some((u) => u.id === data.user_id)) {
              return { ...prev, usernames, lastEvent: event };
            }
            return {
              ...prev,
              users: sortUsers([...prev.users, { id: data.user_id, username }]),
              usernames,
              lastEvent: event,
            };
          }
          if (data.kind === "leave") {
            // For a leave, prefer the frame's username, then the live
            // list (which may have arrived without username on a
            // same-session join), then the seeded directory.
            const fromList = prev.users.find((u) => u.id === data.user_id)?.username ?? "";
            const username =
              frameUsername.length > 0
                ? frameUsername
                : fromList.length > 0
                  ? fromList
                  : (knownUsernames.get(data.user_id) ?? "");
            seq += 1;
            const event: PresenceEvent = {
              kind: "leave",
              id: data.user_id,
              username,
              seq,
            };
            const next = prev.users.filter((u) => u.id !== data.user_id);
            if (next.length === prev.users.length) {
              return { ...prev, usernames, lastEvent: event };
            }
            return { ...prev, users: next, usernames, lastEvent: event };
          }
          return prev;
        });
      });
      try {
        await ws.connect();
      } catch (err) {
        if (tok.cancelled) return;
        const msg = bannerMessage("Presence connection failed", err);
        setState((s) => ({ ...s, error: msg }));
        reportAppError(msg);
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
