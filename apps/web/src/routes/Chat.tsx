import type * as React from "react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import { useAuth } from "../auth/AuthContext.js";
import { useChannels } from "../hooks/useChannels.js";
import { useMessages, type ConnectionState } from "../hooks/useMessages.js";
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

function ConnectionBadge({ state }: { state: ConnectionState }): React.JSX.Element {
  const label =
    state === "open"
      ? "Connected"
      : state === "connecting"
        ? "Connecting..."
        : state === "reconnecting"
          ? "Reconnecting..."
          : state === "closed"
            ? "Disconnected"
            : "Idle";
  return (
    <span className={`conn conn--${state}`} role="status" aria-live="polite">
      {label}
    </span>
  );
}

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
  // Mirror of `activeChannel` for the mount-time focus rAF callback, which
  // captures its scope at mount (when activeChannel is still null) but fires
  // after the channels-list effect has set the first channel.
  const activeChannelRef = useRef<string | null>(activeChannel);
  useEffect(() => {
    activeChannelRef.current = activeChannel;
  }, [activeChannel]);

  useEffect(() => {
    if (activeChannel === null && channelsState.channels.length > 0) {
      setActiveChannel(channelsState.channels[0]?.id ?? null);
    }
  }, [activeChannel, channelsState.channels]);

  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messagesState.messages]);

  // Mount-time focus delivery: composer when a channel is active, else the
  // channel heading, else the message list. Replaces App.tsx's imperative
  // `document.querySelector` chain (issue #189). Sign-out unmounts <Chat />, so
  // a later sign-in re-runs. The composer branch reads `activeChannelRef`
  // (the source of truth driving `disabled` on the textarea) rather than the
  // DOM `disabled` attribute — the ref reflects the latest state when the rAF
  // callback fires after the initial channels-list resolve, where the captured
  // closure value would still be the mount-time `null`.
  useEffect(() => {
    const id = window.requestAnimationFrame(() => {
      const composer = composerRef.current;
      if (composer !== null && activeChannelRef.current !== null) {
        composer.focus();
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
    });
    return () => {
      window.cancelAnimationFrame(id);
    };
  }, []);

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
        {channelsState.loading ? <p>Loading...</p> : null}
        {channelsState.error !== null ? (
          <p role="alert" className="error">
            {channelsState.error}
          </p>
        ) : null}
        <ul aria-label="Channels">
          {channelsState.channels.map((c) => (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => {
                  setActiveChannel(c.id);
                }}
                aria-current={c.id === activeChannel ? "true" : undefined}
              >
                #{c.name}
              </button>
            </li>
          ))}
        </ul>
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
        >
          {messagesState.error !== null ? (
            <p role="alert" className="error">
              {messagesState.error}
            </p>
          ) : null}
          {messagesState.messages.map((m) => {
            const cls =
              m.status === "pending"
                ? "msg msg--pending"
                : m.status === "failed"
                  ? "msg msg--failed"
                  : "msg";
            return (
              <article
                key={m.id}
                className={cls}
                data-testid="msg"
                data-status={m.status ?? "sent"}
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
                        role="status"
                        data-testid="msg-failed-badge"
                      >
                        Failed to send
                      </span>
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
          aria-describedby={showCounter ? "composer-counter" : undefined}
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
              role={overCap ? "alert" : "status"}
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
