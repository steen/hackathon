import type * as React from "react";
import { ConnectionBadge } from "../ConnectionBadge/ConnectionBadge.js";
import type { ConnectionStatus } from "../types.js";
import "./ChannelHeader.css";

interface Props {
  channelName: string | null;
  connectionStatus: ConnectionStatus;
  /** Optional ref forwarded so the parent's focus orchestration can
      target the heading on channel switch. */
  headingRef?: React.Ref<HTMLHeadingElement>;
}

export function ChannelHeader({
  channelName,
  connectionStatus,
  headingRef,
}: Props): React.JSX.Element {
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
      <ConnectionBadge state={connectionStatus} />
    </header>
  );
}
