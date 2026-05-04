import type * as React from "react";
import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
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

  useEffect(() => {
    if (activeChannel === null && channelsState.channels.length > 0) {
      setActiveChannel(channelsState.channels[0]?.id ?? null);
    }
  }, [activeChannel, channelsState.channels]);

  useEffect(() => {
    const el = listRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messagesState.messages]);

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
      <aside className="sidebar">
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
        <ul>
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
        <ul className="presence" aria-label="online users" data-testid="presence-list">
          {presenceState.users.map((u) => (
            <li key={u.id} data-testid={`presence-user-${u.id}`}>
              {u.username.length > 0 ? u.username : u.id}
            </li>
          ))}
        </ul>
      </aside>
      <main className="messages">
        <header className="messages__header">
          <h2>
            {channelsState.channels.find((c) => c.id === activeChannel)?.name ?? "Select a channel"}
          </h2>
          <ConnectionBadge state={messagesState.connection} />
        </header>
        <div
          className="messages__list"
          ref={listRef}
          data-testid="message-list"
          role="log"
          aria-live="polite"
          aria-relevant="additions"
          aria-atomic="false"
          aria-label="conversation"
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
                  <span className="msg__sender">{m.sender_user_id}</span>
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
