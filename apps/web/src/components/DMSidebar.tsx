import type * as React from "react";
import type { Conversation } from "@hackathon/api-client";

// Sidebar section rendering the viewer's DM conversations beneath the
// channel list (decision-log L7). Each row is a button that selects the
// conversation as the active DM thread; the "+" button opens the
// new-DM modal for peer discovery.
//
// Visual structure mirrors ChannelsList for consistency with the
// channels section above. Unread-badge rules:
//   - render only when unread_count > 0 (no zero-badge), display capped
//     at "99+", aria-label exposes the exact count to SR users.
//   - aggregate badge for the "Direct messages" header sums every
//     conversation's unread_count so an idle user sees one number even
//     when no thread is selected.

const UNREAD_DISPLAY_CAP = 99;

function badgeText(n: number): string {
  return n > UNREAD_DISPLAY_CAP ? `${String(UNREAD_DISPLAY_CAP)}+` : String(n);
}

interface Props {
  conversations: Conversation[];
  activeId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
  loading?: boolean;
  error?: string | null;
}

export function DMSidebar(props: Props): React.JSX.Element {
  const { conversations, activeId, onSelect, onNew, loading, error } = props;
  const aggregateUnread = conversations.reduce((sum, c) => sum + c.unread_count, 0);
  const showAggregate = aggregateUnread > 0;

  return (
    <>
      <div className="channels-header">
        <h2>
          Direct messages
          {showAggregate ? (
            <span
              className="channels-list__unread"
              data-testid="dm-aggregate-unread-badge"
              aria-label={`${String(aggregateUnread)} unread DMs`}
              style={{ marginLeft: "0.4rem" }}
            >
              {badgeText(aggregateUnread)}
            </span>
          ) : null}
        </h2>
        <button
          type="button"
          className="channels-header__create"
          onClick={onNew}
          aria-label="Start new DM"
        >
          + New DM
        </button>
      </div>
      {loading === true ? <p>Loading...</p> : null}
      {error !== null && error !== undefined ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : null}
      <ul aria-label="DMs" className="channels-list">
        {conversations.map((c) => {
          const unread = c.unread_count;
          const showBadge = unread > 0;
          const peerName = c.peer.username.length > 0 ? c.peer.username : c.peer.id;
          return (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => {
                  onSelect(c.id);
                }}
                aria-current={c.id === activeId ? "true" : undefined}
                aria-label={showBadge ? `${peerName}, ${String(unread)} unread` : undefined}
              >
                <span className="channels-list__name">{peerName}</span>
                {showBadge ? (
                  <span
                    className="channels-list__unread"
                    data-testid="dm-unread-badge"
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
