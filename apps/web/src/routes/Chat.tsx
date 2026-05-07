import type * as React from "react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { ChannelsList, ConnectionBadge } from "@hackathon/chat-ui";
import { useAuth } from "../auth/AuthContext.js";
import { useChannels } from "../hooks/useChannels.js";
import { useMessages } from "../hooks/useMessages.js";
import { usePresence } from "../hooks/usePresence.js";
import { humanizeTimestamp } from "../utils/formatTimestamp.js";

// Mirrors apps/server/internal/http/messages_handlers.go's MaxMessageBodyBytes
// (4 KiB). The server measures bytes after TrimSpace; the client prevalidates
// in raw bytes so paste-of-large-text gets a warning before the user hits
// Enter rather than after a round-trip.
const MAX_BODY_BYTES = 4 * 1024;
// Above this fraction of the cap, show the user the live byte counter so
// they can see the limit approach. Below it, the chrome stays out of the way.
const WARN_RATIO = 0.8;

// Max distance from the bottom (px) that still counts as "at bottom" for
// the auto-scroll-on-new-message effect. Absorbs subpixel rounding from
// zoom and high-DPI panels; tighter values flicker on iOS Safari, looser
// values miss true near-bottom positions. Exported so tests can derive
// boundary scrollTop values without re-encoding the literal.
export const IS_AT_BOTTOM_TOLERANCE_PX = 8;

function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

export function Chat(): React.JSX.Element {
  const { user, logout } = useAuth();
  const channelsState = useChannels(true);
  const [activeChannel, setActiveChannel] = useState<string | null>(null);
  const messagesState = useMessages(activeChannel, user?.id ?? null);
  const presenceState = usePresence(true);
  const [draft, setDraft] = useState("");
  const composingRef = useRef(false);
  const listRef = useRef<HTMLDivElement | null>(null);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const headingRef = useRef<HTMLHeadingElement | null>(null);
  // Once the focus anchor lands on the composer, leave it alone — re-running
  // the effect on later state changes would steal focus from wherever the
  // user has navigated since (e.g. the channel list). The heading/list
  // branches are first-paint placeholders and remain re-targetable until the
  // composer becomes the resting anchor.
  const composerFocusedRef = useRef(false);

  useEffect(() => {
    if (activeChannel === null && channelsState.channels.length > 0) {
      setActiveChannel(channelsState.channels[0]?.id ?? null);
    }
  }, [activeChannel, channelsState.channels]);

  // Auto-scroll only when the user is already pinned to the bottom. If they
  // scrolled up to read history, a new live message must not yank them back —
  // mid-thread reading is the common mobile case (#633, parent #156).
  // `scrollHeight` is read *before* the next paint, so the check races the
  // layout that adds the new row — but the previous render already pinned
  // `scrollTop` to the prior bottom whenever the user was there, so the
  // comparison is correct against the pre-update geometry.
  const wasAtBottomRef = useRef(true);
  const onListScroll = useCallback((): void => {
    const el = listRef.current;
    if (el === null) return;
    const distanceFromBottom = el.scrollHeight - (el.scrollTop + el.clientHeight);
    wasAtBottomRef.current = distanceFromBottom <= IS_AT_BOTTOM_TOLERANCE_PX;
  }, []);
  useEffect(() => {
    const el = listRef.current;
    if (el === null) return;
    if (wasAtBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messagesState.messages]);

  // Focus delivery, priority composer → heading → list. useLayoutEffect lands
  // focus before the browser paints so SR users don't see a frame on
  // `document.body`. Re-runs when `activeChannel` flips from null to a real
  // id (e.g. once `useChannels` resolves), promoting focus from the heading
  // placeholder to the composer. `composerFocusedRef` guards against
  // stealing focus back if the user has tabbed away since.
  useLayoutEffect(() => {
    if (composerFocusedRef.current) return;
    const composer = composerRef.current;
    if (composer !== null && activeChannel !== null) {
      composer.focus();
      composerFocusedRef.current = true;
      return;
    }
    const heading = headingRef.current;
    if (heading !== null) {
      heading.focus();
      return;
    }
    const list = listRef.current;
    if (list !== null) {
      list.focus();
    }
  }, [activeChannel]);

  // Build the polite-region announcement text from the latest presence
  // event. The presence list itself reorders rather than appends rows, so
  // SR users don't get an aria-live additions announcement from the list —
  // we mirror the event into a sibling status region instead. When the
  // username is unknown (live event for an id not in the seeded directory)
  // the phrase elides the id rather than reading out a UUID. The fallback
  // differs by kind: "a new user" reads naturally for joins but is
  // grammatically odd for leaves (the leaver isn't new from the listener's
  // frame), so unknown leaves drop "new" — see issue #495.
  const presenceAnnouncement = useMemo<string>(() => {
    const ev = presenceState.lastEvent;
    if (ev === null) return "";
    if (ev.username.length > 0) {
      return ev.kind === "join" ? `${ev.username} joined` : `${ev.username} left`;
    }
    return ev.kind === "join" ? "a new user joined" : "a user left";
  }, [presenceState.lastEvent]);

  // Auth user before presence: own messages render correctly even before
  // the /api/presence seed lands. Falls back to the raw id so an unknown
  // sender (history from a user who has since left) doesn't crash — #148.
  // Memoized so a future per-message memoized child can rely on a stable
  // reference identity across renders (#535).
  const resolveSender = useCallback(
    (id: string): string => {
      if (user !== null && user.id === id) return user.username;
      const known = presenceState.usernames.get(id);
      if (known !== undefined && known.length > 0) return known;
      return id;
    },
    [user, presenceState.usernames],
  );

  const activeChannelName = useMemo<string | null>(() => {
    if (activeChannel === null) return null;
    return channelsState.channels.find((c) => c.id === activeChannel)?.name ?? null;
  }, [activeChannel, channelsState.channels]);

  const draftBytes = useMemo(() => byteLength(draft), [draft]);
  const overCap = draftBytes > MAX_BODY_BYTES;
  const showCounter = draftBytes >= Math.floor(MAX_BODY_BYTES * WARN_RATIO);
  const trimmedEmpty = draft.trim().length === 0;
  const sendDisabled = activeChannel === null || trimmedEmpty || overCap;

  // Empty-state surfaces only after the initial channel fetch settles (no
  // loading, no error). Showing the "no channels" copy mid-load would race
  // the eventual list and flash for SR users.
  const showNoChannelsEmpty =
    !channelsState.loading && channelsState.error === null && channelsState.channels.length === 0;
  // Mirrors the no-channels guard above: hold the hint until the initial
  // listMessages fetch settles. Otherwise the connecting → connected window
  // (state is `messages === []`, `error === null`) flashes the hint for the
  // duration of the fetch on every channel switch.
  const showEmptyChannelHint =
    activeChannel !== null &&
    !messagesState.historyLoading &&
    messagesState.error === null &&
    messagesState.messages.length === 0;

  async function submitDraft(): Promise<void> {
    if (sendDisabled) return;
    const body = draft.trim();
    if (body.length === 0) return;
    setDraft("");
    await messagesState.send(body);
  }

  function onSend(e: FormEvent<HTMLFormElement>): void {
    e.preventDefault();
    void submitDraft();
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>): void {
    // Enter sends; Shift+Enter inserts a newline. IME composition (Japanese,
    // Chinese, Korean) fires Enter to commit a candidate — never treat that
    // as a send.
    if (e.key !== "Enter") return;
    if (e.shiftKey) return;
    if (composingRef.current) return;
    // `isComposing` is the canonical flag for browsers that emit it; the
    // ref handles older fallbacks. Keep both.
    if (e.nativeEvent.isComposing) return;
    e.preventDefault();
    void submitDraft();
  }

  return (
    <div className="chat-layout">
      <aside className="sidebar" aria-label="Chat sidebar">
        <header>
          <strong>{user?.username ?? "..."}</strong>
          <button
            type="button"
            onClick={() => {
              void logout();
            }}
          >
            Sign out
          </button>
        </header>
        <h2>Channels</h2>
        <ChannelsList
          channels={channelsState.channels}
          activeId={activeChannel}
          onSelect={setActiveChannel}
          loading={channelsState.loading}
          error={channelsState.error}
        />
        <h2>Online</h2>
        <ul className="presence" aria-label="Online users" data-testid="presence-list">
          {presenceState.users.map((u) => (
            <li key={u.id} data-testid={`presence-user-${u.id}`}>
              {u.username.length > 0 ? u.username : u.id}
            </li>
          ))}
        </ul>
        {/* aria-live="polite" alone is the load-bearing announcement
            mechanism; an explicit role="status" is omitted so the
            element doesn't collide with `getByRole("status")` queries
            already used by the connection badge (the e2e
            `page.getByRole("status")` locator expects exactly one
            match). aria-atomic="true" so the SR re-reads the whole
            phrase on each event, not just the diff. */}
        <div
          className="visually-hidden"
          aria-live="polite"
          aria-atomic="true"
          data-testid="presence-live-region"
        >
          {presenceAnnouncement}
        </div>
      </aside>
      <main className="messages" aria-label={activeChannelName ?? "Messages"}>
        <header className="messages__header">
          <h2 ref={headingRef} tabIndex={-1}>
            {activeChannelName ?? "Select a channel"}
          </h2>
          <ConnectionBadge state={messagesState.connection} />
        </header>
        {/* role="log" implies aria-live="polite" per ARIA 1.2 — single
            source of truth so a future flip to assertive only needs the
            role change. aria-relevant/aria-atomic stay explicit because
            they override the role's defaults. */}
        <div
          className="messages__list"
          ref={listRef}
          data-testid="message-list"
          role="log"
          aria-relevant="additions"
          aria-atomic="false"
          aria-label="conversation"
          tabIndex={-1}
          onScroll={onListScroll}
        >
          {messagesState.error !== null ? (
            <p role="alert" className="error">
              {messagesState.error}
            </p>
          ) : null}
          {showNoChannelsEmpty ? (
            <p className="empty-state" data-testid="empty-state-no-channels">
              No channels available yet. Wait for an admin to create one.
            </p>
          ) : null}
          {showEmptyChannelHint && activeChannelName !== null ? (
            <p className="empty-state" data-testid="empty-state-channel-hint">
              {`This is the start of #${activeChannelName} — send a message to say hi.`}
            </p>
          ) : null}
          {messagesState.canLoadOlder ? (
            <>
              <button
                type="button"
                className="messages__load-older"
                data-testid="load-older-button"
                onClick={() => {
                  void messagesState.loadOlder();
                }}
                disabled={messagesState.isLoadingOlder}
                aria-busy={messagesState.isLoadingOlder ? "true" : undefined}
              >
                {messagesState.isLoadingOlder ? "Loading older messages…" : "Load older messages"}
              </button>
              {messagesState.loadOlderError !== null ? (
                <p
                  role="alert"
                  className="error messages__load-older-error"
                  data-testid="load-older-error"
                >
                  {messagesState.loadOlderError}
                </p>
              ) : null}
            </>
          ) : null}
          {messagesState.messages.map((m) => {
            const cls =
              m.status === "pending"
                ? "msg msg--pending"
                : m.status === "failed"
                  ? "msg msg--failed"
                  : "msg";
            // Suppress SR announcement of the user's own messages — the
            // optimistic-send path appends them immediately on submit, and
            // SR users typed them so the polite-log readback is annoying
            // (#139, #468). The WS echo (`useMessages` reconcile) reuses
            // the same pending row, so the article keeps aria-hidden once
            // its sender resolves to self. Failed-status rows stay
            // announceable (the failed-badge `role="status"` plus the
            // Retry button must remain in the a11y tree) — failed-send
            // SR behaviour is tracked separately as #147.
            const isSelf = user !== null && m.sender_user_id === user.id;
            const ariaHidden = isSelf && m.status !== "failed" ? "true" : undefined;
            return (
              <article
                key={m.id}
                className={cls}
                data-testid="msg"
                data-status={m.status ?? "sent"}
                aria-hidden={ariaHidden}
              >
                <div className="msg__meta">
                  <span className="msg__sender">{resolveSender(m.sender_user_id)}</span>
                  {m.status === "pending" ? (
                    <span className="msg__badge msg__badge--pending" role="status">
                      Sending…
                    </span>
                  ) : null}
                  {m.status === "pending" || m.created_at.length === 0 ? null : (
                    <time dateTime={m.created_at}>{humanizeTimestamp(m.created_at)}</time>
                  )}
                  {m.status === "failed" ? (
                    <>
                      <span
                        className="msg__badge msg__badge--error"
                        role="alert"
                        data-testid="msg-failed-badge"
                        aria-describedby={
                          m.failureReason !== undefined && m.failureReason.length > 0
                            ? `msg-failed-reason-${m.id}`
                            : undefined
                        }
                      >
                        Failed to send
                      </span>
                      {m.failureReason !== undefined && m.failureReason.length > 0 ? (
                        <span
                          id={`msg-failed-reason-${m.id}`}
                          className="msg__failure-reason"
                          data-testid="msg-failed-reason"
                        >
                          {m.failureReason}
                        </span>
                      ) : null}
                      <button
                        type="button"
                        className="msg__retry"
                        onClick={() => {
                          void messagesState.retry(m.id);
                        }}
                      >
                        Retry
                      </button>
                    </>
                  ) : null}
                </div>
                <div className="msg__body">{m.body}</div>
              </article>
            );
          })}
        </div>
        <form
          className="composer"
          onSubmit={onSend}
          aria-describedby={showCounter && !overCap ? "composer-counter" : undefined}
        >
          <textarea
            ref={composerRef}
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value);
            }}
            onKeyDown={onKeyDown}
            onCompositionStart={() => {
              composingRef.current = true;
            }}
            onCompositionEnd={() => {
              composingRef.current = false;
            }}
            placeholder={activeChannel === null ? "Select a channel first" : "Write a message..."}
            disabled={activeChannel === null}
            aria-label="message"
            aria-invalid={overCap || undefined}
            aria-errormessage={overCap ? "composer-counter" : undefined}
            rows={2}
            data-testid="composer-textarea"
          />
          <button type="submit" disabled={sendDisabled}>
            Send
          </button>
          {showCounter ? (
            <span
              id="composer-counter"
              className={
                overCap
                  ? "composer__counter composer__counter--error"
                  : "composer__counter composer__counter--warn"
              }
              role="status"
              data-testid="composer-counter"
            >
              {draftBytes} / {MAX_BODY_BYTES} bytes
              {overCap ? " — too long to send" : ""}
            </span>
          ) : null}
        </form>
      </main>
    </div>
  );
}
