import type * as React from "react";
import { useCallback, useEffect, useRef } from "react";
import { MessageItem } from "../MessageItem/MessageItem.js";
import type { ChatMessage } from "../types.js";
import "./MessageList.css";

// Max distance from the bottom (px) that still counts as "at bottom" for
// the auto-scroll-on-new-message effect. Absorbs subpixel rounding from
// zoom and high-DPI panels; tighter values flicker on iOS Safari, looser
// values miss true near-bottom positions. Exported so unit tests can
// derive boundary scrollTop values without re-encoding the literal.
export const IS_AT_BOTTOM_TOLERANCE_PX = 8;

interface Props {
  messages: ChatMessage[];
  resolveSender: (id: string) => string;
  selfUserId?: string | null;
  /** Top-level fetch error banner. */
  error?: string | null;
  /** Optional empty-state nodes. Both render unconditionally when their
      respective showXxx flags resolve true; consumer decides the order. */
  emptyState?: React.ReactNode;
  showNoChannelsEmpty?: boolean;
  showEmptyChannelHint?: boolean;
  emptyChannelHintText?: string;
  /** Pagination affordances. */
  canLoadOlder?: boolean;
  isLoadingOlder?: boolean;
  loadOlderError?: string | null;
  onLoadOlder?: () => void;
  /** Optional ref forwarded to the scroll container so the consumer can
      target it for cross-component focus orchestration. The component
      always attaches its own ref internally for scroll tracking. */
  listRef?: React.Ref<HTMLDivElement>;
  /** Retry a failed send; receives the message id. */
  onRetry?: (messageId: string) => void;
}

function setRef<T>(ref: React.Ref<T> | undefined, value: T | null): void {
  if (typeof ref === "function") ref(value);
  else if (ref !== null && ref !== undefined) {
    (ref as React.MutableRefObject<T | null>).current = value;
  }
}

export function MessageList(props: Props): React.JSX.Element {
  const {
    messages,
    resolveSender,
    selfUserId,
    error,
    showNoChannelsEmpty,
    showEmptyChannelHint,
    emptyChannelHintText,
    canLoadOlder,
    isLoadingOlder,
    loadOlderError,
    onLoadOlder,
    listRef,
    onRetry,
  } = props;

  const internalRef = useRef<HTMLDivElement | null>(null);
  const wasAtBottomRef = useRef(true);

  const onScroll = useCallback((): void => {
    const el = internalRef.current;
    if (el === null) return;
    const distanceFromBottom = el.scrollHeight - (el.scrollTop + el.clientHeight);
    wasAtBottomRef.current = distanceFromBottom <= IS_AT_BOTTOM_TOLERANCE_PX;
  }, []);

  // Auto-scroll only when the user is already pinned to the bottom. Mid-thread
  // reading must not yank the viewport to the latest message (#633, #156).
  // `scrollHeight` is read *before* the next paint, so the check races the
  // layout that adds the new row — but the previous render already pinned
  // `scrollTop` to the prior bottom whenever the user was there, so the
  // comparison is correct against the pre-update geometry.
  useEffect(() => {
    const el = internalRef.current;
    if (el === null) return;
    if (wasAtBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages]);

  const handleRef = useCallback(
    (el: HTMLDivElement | null): void => {
      internalRef.current = el;
      setRef(listRef, el);
    },
    [listRef],
  );

  return (
    <div
      className="messages__list"
      ref={handleRef}
      data-testid="message-list"
      role="log"
      aria-relevant="additions"
      aria-atomic="false"
      aria-label="conversation"
      tabIndex={-1}
      onScroll={onScroll}
    >
      {error !== null && error !== undefined ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : null}
      {showNoChannelsEmpty === true ? (
        <p className="empty-state" data-testid="empty-state-no-channels">
          No channels available yet. Wait for an admin to create one.
        </p>
      ) : null}
      {showEmptyChannelHint === true && emptyChannelHintText !== undefined ? (
        <p className="empty-state" data-testid="empty-state-channel-hint">
          {emptyChannelHintText}
        </p>
      ) : null}
      {canLoadOlder === true ? (
        <>
          <button
            type="button"
            className="messages__load-older"
            data-testid="load-older-button"
            onClick={onLoadOlder}
            disabled={isLoadingOlder === true}
            aria-busy={isLoadingOlder === true ? "true" : undefined}
          >
            {isLoadingOlder === true
              ? "Loading older messages…"
              : "Load older messages"}
          </button>
          {loadOlderError !== null && loadOlderError !== undefined ? (
            <p
              role="alert"
              className="error messages__load-older-error"
              data-testid="load-older-error"
            >
              {loadOlderError}
            </p>
          ) : null}
        </>
      ) : null}
      {messages.map((m) => {
        // Suppress SR announcement of the user's own messages — the optimistic-
        // send path appends them immediately on submit, and SR users typed them
        // so the polite-log readback is annoying (#139, #468). Failed-status
        // rows stay announceable.
        const isSelf =
          selfUserId !== null && selfUserId !== undefined && m.sender_user_id === selfUserId;
        const ariaHidden = isSelf && m.status !== "failed";
        return (
          <MessageItem
            key={m.id}
            sender={resolveSender(m.sender_user_id)}
            senderId={m.sender_user_id}
            body={m.body}
            createdAt={m.created_at}
            status={m.status}
            failureReason={m.failureReason}
            ariaHidden={ariaHidden}
            reasonId={`msg-failed-reason-${m.id}`}
            onRetry={
              onRetry !== undefined
                ? () => {
                    onRetry(m.id);
                  }
                : undefined
            }
          />
        );
      })}
    </div>
  );
}
