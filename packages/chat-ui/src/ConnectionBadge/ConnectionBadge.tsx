import type * as React from "react";
import type { ConnectionStatus } from "../types.js";
import "./ConnectionBadge.css";

const LABELS: Record<ConnectionStatus, string> = {
  open: "Connected",
  connecting: "Connecting...",
  reconnecting: "Reconnecting...",
  closed: "Disconnected",
  idle: "Idle",
};

interface Props {
  state: ConnectionStatus;
}

export function ConnectionBadge({ state }: Props): React.JSX.Element {
  return (
    <span className={`conn conn--${state}`} role="status" aria-live="polite">
      {LABELS[state]}
    </span>
  );
}
