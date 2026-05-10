import { useEffect, useMemo, useRef, useState } from "react";
import type { ChannelEvent, Event as WsEvent, WrapsNeededResponse } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";
import type { ChatSocket } from "./useChatSocket.js";

// L31 — clients debounce: wraps-needed is queried ONCE per WS-connection
// lifetime per channel; flapping reconnects do not re-query unless > 60s
// have elapsed since the last query.
const LAZY_WRAP_DEBOUNCE_MS = 60_000;

// ChannelKeyStatus is the per-channel state surface the hook returns.
// `waitingForKey` is true when the GET /wraps-needed response includes
// the viewer's own user_id in `missing` (= the viewer's wrap is the one
// the lazy-wrap loop has not filled in yet); the UI renders "waiting
// for key…" until a `{type:"channel",data:{kind:"key_received",...}}`
// frame arrives for that channel.
export interface ChannelKeyStatus {
  waitingForKey: boolean;
  generationID: number | null;
  missingCount: number;
}

export interface UseChannelKeys {
  /** Per-channel key state, indexed by channel id. */
  status: Readonly<Record<string, ChannelKeyStatus>>;
}

interface UseChannelKeysOpts {
  /** Authenticated viewer's user id. Hook is inert when unset. */
  viewerUserID: string | null;
  /** Channels the viewer is a member of. Drives the per-channel fetch. */
  channelIDs: string[];
  /** Shared chat socket. Hook subscribes to `open` (re-fetch trigger)
   *  and `message` (key_received clear) on it. */
  socket?: ChatSocket;
}

interface PerChannelEntry {
  status: ChannelKeyStatus;
  // Last time we fired the wraps-needed call for this channel. Used
  // to enforce the L31 60s debounce across socket-open events.
  lastQueryAt: number;
}

function isChannelEvent(ev: WsEvent): ev is ChannelEvent {
  return ev.type === "channel";
}

function buildStatus(resp: WrapsNeededResponse, viewerUserID: string): ChannelKeyStatus {
  const own = resp.missing.find((row) => row.user_id === viewerUserID);
  return {
    waitingForKey: own !== undefined,
    generationID: own?.generation_id ?? null,
    missingCount: resp.missing.length,
  };
}

// useChannelKeys runs the L14 lazy-wrap-on-online query for each
// channel the viewer is a member of: on every WS open, it fetches
// `GET /api/channels/{id}/members/wraps-needed` (L31 debounced) and
// stores the per-channel status. When a `key_received` WS frame
// arrives for a channel, the status' waitingForKey flag flips to
// false so the UI clears "waiting for key…" without a refetch.
//
// The actual wrap computation + POST loop is intentionally NOT here —
// the viewer's missing wraps are filled in by OTHER online members'
// clients (the `missing` row gets a wrap fan-out via key_received).
// The viewer's own client only computes wraps for OTHERS who are
// missing one, which requires the channel root-key plaintext (only
// available after the encrypted-message decrypt loop in #983 lands).
// This hook ships the structural read + UI state today; the compute-
// and-post arm follows when #983 wires the decrypt path.
export function useChannelKeys(opts: UseChannelKeysOpts): UseChannelKeys {
  const { viewerUserID, channelIDs, socket } = opts;
  const [statusMap, setStatusMap] = useState<Record<string, ChannelKeyStatus>>({});
  const trackerRef = useRef<Record<string, PerChannelEntry>>({});

  const channelKey = useMemo(() => channelIDs.join("|"), [channelIDs]);

  useEffect(() => {
    if (viewerUserID === null) return;
    if (socket === undefined) return;

    let cancelled = false;
    const fetchOnce = (channelID: string): void => {
      const now = Date.now();
      const prev = trackerRef.current[channelID];
      if (prev !== undefined && now - prev.lastQueryAt < LAZY_WRAP_DEBOUNCE_MS) {
        return;
      }
      trackerRef.current[channelID] = {
        status: prev?.status ?? {
          waitingForKey: false,
          generationID: null,
          missingCount: 0,
        },
        lastQueryAt: now,
      };
      void getClient()
        .http.wrapsNeeded(channelID)
        .then((resp) => {
          if (cancelled) return;
          const next = buildStatus(resp, viewerUserID);
          trackerRef.current[channelID] = {
            status: next,
            lastQueryAt: trackerRef.current[channelID]?.lastQueryAt ?? now,
          };
          setStatusMap((s) => ({ ...s, [channelID]: next }));
        })
        .catch((err: unknown) => {
          // 403 (not a member yet — race) and 404 (channel removed)
          // are non-fatal; other errors surface so the supervisor can
          // diagnose. We reset the debounce timer so the next socket-
          // open will retry.
          trackerRef.current[channelID] = {
            status: trackerRef.current[channelID]?.status ?? {
              waitingForKey: false,
              generationID: null,
              missingCount: 0,
            },
            lastQueryAt: 0,
          };
          reportAppError(bannerMessage("Failed to load channel keys", err));
        });
    };

    // Drive the initial query for every channel synchronously on mount
    // (the socket may already be open; the open subscriber below picks
    // up subsequent reconnects). Subsequent channelIDs changes also
    // re-run via channelKey in the dep array.
    for (const id of channelIDs) {
      fetchOnce(id);
    }

    const offOpen = socket.subscribe("open", () => {
      for (const id of channelIDs) {
        fetchOnce(id);
      }
    });

    // L9 key_received → clear the per-channel waitingForKey flag
    // without a refetch. The frame is fanned to user:<viewer> so we
    // can trust it pertains to this viewer.
    const offMessage = socket.subscribe("message", (ev) => {
      if (!isChannelEvent(ev)) return;
      if (ev.data.kind !== "key_received") return;
      const channelID = ev.data.channel_id;
      const generation = ev.data.generation_id;
      setStatusMap((s) => {
        const prev = s[channelID];
        if (prev === undefined) return s;
        return {
          ...s,
          [channelID]: {
            waitingForKey: false,
            generationID: generation,
            missingCount: Math.max(0, prev.missingCount - 1),
          },
        };
      });
      const tracked = trackerRef.current[channelID];
      if (tracked !== undefined) {
        trackerRef.current[channelID] = {
          status: {
            waitingForKey: false,
            generationID: generation,
            missingCount: Math.max(0, tracked.status.missingCount - 1),
          },
          lastQueryAt: tracked.lastQueryAt,
        };
      }
    });

    return () => {
      cancelled = true;
      offOpen();
      offMessage();
    };
  }, [viewerUserID, socket, channelKey, channelIDs]);

  return { status: statusMap };
}
