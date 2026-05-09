import type * as React from "react";
import "./ChannelsList.css";

export interface Channel {
  id: string;
  name: string;
  /** Server-tracked unread count for the viewer (Phase 9). Optional under
   *  the wire-types optional-first rule (specs/plans/phase-9/read-state.md
   *  L26): legacy fixtures and tests that omit this field still compile,
   *  and `undefined` renders no badge. Treated as 0 for rendering. */
  unread_count?: number;
}

interface Props {
  channels: Channel[];
  activeId: string | null;
  onSelect: (id: string) => void;
  loading?: boolean;
  error?: string | null;
}

// Cap displayed unread counts at 99+ so a long-idle viewer doesn't blow
// out the sidebar layout. Matches the Slack/Discord convention; the exact
// number lives on hover/focus via the aria-label so SR users still hear
// "23 unread" rather than the truncated text.
// keep in sync with the "99+" literal in badgeText below
const UNREAD_DISPLAY_CAP = 99;

function badgeText(n: number): string {
  return n > UNREAD_DISPLAY_CAP ? "99+" : String(n);
}

export function ChannelsList({
  channels,
  activeId,
  onSelect,
  loading,
  error,
}: Props): React.JSX.Element {
  return (
    <>
      {loading === true ? <p>Loading...</p> : null}
      {error !== null && error !== undefined ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : null}
      <ul aria-label="Channels" className="channels-list">
        {channels.map((c) => {
          const unread = c.unread_count ?? 0;
          const showBadge = unread > 0;
          return (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => {
                  onSelect(c.id);
                }}
                aria-current={c.id === activeId ? "true" : undefined}
                aria-label={showBadge ? `#${c.name}, ${String(unread)} unread` : undefined}
              >
                <span className="channels-list__name">#{c.name}</span>
                {showBadge ? (
                  <span
                    className="channels-list__unread"
                    data-testid="channel-unread-badge"
                    aria-hidden="true"
                  >
                    {badgeText(unread)}
                  </span>
                ) : null}
              </button>
            </li>
          );
        })}
      </ul>
    </>
  );
}
