import type * as React from "react";
import { userColorClass } from "../colorize.js";
import type { MessageStatus } from "../types.js";
import { humanizeTimestamp } from "./humanizeTimestamp.js";

interface Props {
  sender: string;
  senderId: string;
  body: string;
  createdAt: string;
  /** Optimistic-send state. Absent ≡ the message was successfully sent
      and persisted; the row renders with no status badge. The
      `data-status` attribute on the article reflects this as
      "pending" | "failed" | "sent" so existing E2E selectors keep
      working. */
  status?: MessageStatus;
  failureReason?: string;
  onRetry?: () => void;
  ariaHidden?: boolean;
  /** Stable id for `aria-describedby` linkage on the failure-reason span. */
  reasonId?: string;
}

// Renders one message row.
//
// Layout (matches the reference screenshot — meta line is `<time> <sender>
// <badges>`, body on the next line):
//
//   <article class="msg" data-status=...>
//     <div class="msg__meta">
//       <time>12:01</time>  user1  Sending...   Failed to send  Retry
//     </div>
//     <div class="msg__body">hey, anyone here?</div>
//   </article>
//
// Class names + data-testid + role attrs are part of the test contract
// (apps/web/src/routes/Chat.test.tsx queries by `.msg__body`, `.msg__sender`,
// `[data-testid=msg]`, `[data-status]`, etc.) — preserve verbatim.
export function MessageItem({
  sender,
  senderId,
  body,
  createdAt,
  status,
  failureReason,
  onRetry,
  ariaHidden,
  reasonId,
}: Props): React.JSX.Element {
  const cls =
    status === "pending" ? "msg msg--pending" : status === "failed" ? "msg msg--failed" : "msg";
  const dataStatus = status ?? "sent";
  const senderColor = userColorClass(senderId);
  const showTime = status !== "pending" && createdAt.length > 0;
  const reasonRendered = failureReason !== undefined && failureReason.length > 0;

  return (
    <article
      className={cls}
      data-testid="msg"
      data-status={dataStatus}
      aria-hidden={ariaHidden === true ? "true" : undefined}
    >
      <div className="msg__meta">
        {showTime ? <time dateTime={createdAt}>{humanizeTimestamp(createdAt)}</time> : null}
        <span className={`msg__sender ${senderColor}`}>{sender}</span>
        {status === "pending" ? (
          <span className="msg__badge msg__badge--pending" role="status">
            Sending…
          </span>
        ) : null}
        {status === "failed" ? (
          <>
            <span
              className="msg__badge msg__badge--error"
              role="alert"
              data-testid="msg-failed-badge"
              aria-describedby={reasonRendered ? reasonId : undefined}
            >
              Failed to send
            </span>
            {reasonRendered ? (
              <span id={reasonId} className="msg__failure-reason" data-testid="msg-failed-reason">
                {failureReason}
              </span>
            ) : null}
            <button type="button" className="msg__retry" onClick={onRetry}>
              Retry
            </button>
          </>
        ) : null}
      </div>
      <div className="msg__body">{body}</div>
    </article>
  );
}
