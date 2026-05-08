import { useCallback, useEffect, useRef } from "react";
import { markChannelRead, markDMRead } from "@hackathon/api-client";
import { getClient } from "../api.js";
import { bannerMessage, reportAppError } from "../lib/userFacingError.js";

// Debounced read-pointer advance for either a channel or a DM.
//
// Decision-log L22 (lt -p direct-messages 3 §22): markRead debounces 250ms
// trailing — rapid calls collapse to one outgoing POST so a fast scroll
// doesn't fan a request per message id. Explicit `flush()` (focus return,
// component unmount, manual user action) drops the wait and posts the most
// recent message id immediately.
//
// Server semantics (channels.ts / dms.ts): both POST endpoints are
// advance-only (decision-log L5) — a stale message id is a 200 no-op,
// so a debounce window that drops intermediate ids is safe by
// construction. The caller passes monotonically-newest message ids on the
// happy path; out-of-order calls are tolerated server-side.

export const READ_MARKER_DEBOUNCE_MS = 250;

export type ReadMarkerScope = "channel" | "dm";

export interface UseReadMarker {
  /** Schedule a trailing-debounced read-pointer advance for `messageId`. */
  markRead: (messageId: string) => void;
  /** Fire any pending advance immediately, bypassing the debounce timer. */
  flush: () => void;
}

export function useReadMarker(scope: ReadMarkerScope, scopeId: string): UseReadMarker {
  // Hold the most-recent pending id in a ref so successive calls within
  // the debounce window collapse without rerendering the consumer.
  const pendingRef = useRef<string | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const scopeRef = useRef({ scope, scopeId });

  // Track the current scope so a flush triggered by scope change posts to
  // the previous scope (the id the user actually saw) rather than the
  // newly-mounted one.
  useEffect(() => {
    scopeRef.current = { scope, scopeId };
  }, [scope, scopeId]);

  const post = useCallback((s: ReadMarkerScope, id: string, messageId: string): void => {
    const http = getClient().http;
    const promise =
      s === "channel" ? markChannelRead(http, id, messageId) : markDMRead(http, id, messageId);
    promise.catch((err: unknown) => {
      const label = s === "channel" ? "Failed to mark channel read" : "Failed to mark DM read";
      reportAppError(bannerMessage(label, err));
    });
  }, []);

  const flush = useCallback((): void => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    const messageId = pendingRef.current;
    if (messageId === null) return;
    pendingRef.current = null;
    const { scope: s, scopeId: id } = scopeRef.current;
    post(s, id, messageId);
  }, [post]);

  const markRead = useCallback(
    (messageId: string): void => {
      pendingRef.current = messageId;
      if (timerRef.current !== null) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        const pending = pendingRef.current;
        if (pending === null) return;
        pendingRef.current = null;
        const { scope: s, scopeId: id } = scopeRef.current;
        post(s, id, pending);
      }, READ_MARKER_DEBOUNCE_MS);
    },
    [post],
  );

  // Flush any pending advance when the scope changes or the component
  // unmounts so a user navigating away doesn't drop the read pointer.
  useEffect(() => {
    return () => {
      flush();
    };
  }, [scope, scopeId, flush]);

  // Browser focus return is an explicit "user is back" signal — flush
  // any pending advance so the unread count drops immediately rather
  // than waiting out the 250ms timer. `visibilitychange` to "visible"
  // covers tab-switch return; `focus` covers window focus return on
  // platforms where visibility doesn't fire (legacy Safari quirks).
  // The listeners are no-ops outside the browser (SSR / vitest jsdom
  // without document.addEventListener) — both globals are guarded.
  useEffect(() => {
    if (typeof document === "undefined") return;
    const onVisibility = (): void => {
      if (document.visibilityState === "visible") flush();
    };
    const onFocus = (): void => {
      flush();
    };
    document.addEventListener("visibilitychange", onVisibility);
    if (typeof window !== "undefined") {
      window.addEventListener("focus", onFocus);
    }
    return () => {
      document.removeEventListener("visibilitychange", onVisibility);
      if (typeof window !== "undefined") {
        window.removeEventListener("focus", onFocus);
      }
    };
  }, [flush]);

  return { markRead, flush };
}
