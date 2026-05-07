import type * as React from "react";
import "./ChannelHeader.css";

interface Props {
  channelName: string | null;
  /** Optional ref forwarded so the parent's focus orchestration can
      target the heading on channel switch. */
  headingRef?: React.Ref<HTMLHeadingElement>;
}

// Channel-name strip above the message log. Connection status used to
// live here as a `<ConnectionBadge>`; the TopBar now owns the single
// "Online"/"Offline" status indicator (with role="status"), so this
// header is back to a plain title row.
export function ChannelHeader({ channelName, headingRef }: Props): React.JSX.Element {
  return (
    <header className="messages__header">
      <h2 ref={headingRef} tabIndex={-1} className="messages__title">
        {channelName !== null ? (
          <>
            <span className="messages__title-hash" aria-hidden="true">
              #
            </span>
            {channelName}
          </>
        ) : (
          "Select a channel"
        )}
      </h2>
    </header>
  );
}
