import type * as React from "react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useAuth } from "../auth/AuthContext.js";
import { useChannels } from "../hooks/useChannels.js";
import { useMessages, type ConnectionState } from "../hooks/useMessages.js";
import { usePresence } from "../hooks/usePresence.js";

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

export function Chat(): React.JSX.Element {
  const { user, logout } = useAuth();
  const channelsState = useChannels(true);
  const [activeChannel, setActiveChannel] = useState<string | null>(null);
  const messagesState = useMessages(activeChannel, user?.id ?? null);
  const presenceState = usePresence(true);
  const [draft, setDraft] = useState("");
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

  async function onSend(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    if (activeChannel === null) return;
    const body = draft.trim();
    if (body.length === 0) return;
    setDraft("");
    await messagesState.send(body);
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
        <div className="messages__list" ref={listRef} data-testid="message-list">
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
            const style = m.status === "pending" ? { opacity: 0.6 } : undefined;
            return (
              <article
                key={m.id}
                className={cls}
                style={style}
                data-testid="msg"
                data-status={m.status ?? "sent"}
              >
                <div className="msg__meta">
                  <span className="msg__sender">{m.sender_user_id}</span>
                  {m.status === "pending" || m.created_at.length === 0 ? null : (
                    <time dateTime={m.created_at}>{m.created_at}</time>
                  )}
                  {m.status === "failed" ? (
                    <>
                      <span className="msg__badge msg__badge--error" role="status">
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
          onSubmit={(e) => {
            void onSend(e);
          }}
        >
          <input
            type="text"
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value);
            }}
            placeholder={activeChannel === null ? "Select a channel first" : "Write a message..."}
            disabled={activeChannel === null}
            aria-label="message"
          />
          <button type="submit" disabled={activeChannel === null || draft.trim().length === 0}>
            Send
          </button>
        </form>
      </main>
    </div>
  );
}
