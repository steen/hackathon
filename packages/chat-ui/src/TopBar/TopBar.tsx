import type * as React from "react";
import { userColorClass } from "../colorize.js";
import "./TopBar.css";

interface TopBarUser {
  id: string;
  username: string;
}

interface Props {
  workspaceName: string;
  user: TopBarUser;
  online: boolean;
  /** Optional click handlers; when undefined, the cluster renders as a
      static `<div>` instead of a dead `<button>`. The chevron stays in
      either case as a visual affordance. */
  onWorkspaceClick?: () => void;
  onUserClick?: () => void;
}

function Avatar({ user }: { user: TopBarUser }): React.JSX.Element {
  const initial = user.username.charAt(0).toUpperCase() || "?";
  return (
    <span
      className={`top-bar__avatar ${userColorClass(user.id)}`}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

export function TopBar({
  workspaceName,
  user,
  online,
  onWorkspaceClick,
  onUserClick,
}: Props): React.JSX.Element {
  const dotClass = online ? "top-bar__dot top-bar__dot--online" : "top-bar__dot";

  const left = (
    <>
      <span className="top-bar__hash" aria-hidden="true">
        #
      </span>
      <span className="top-bar__workspace-name">{workspaceName}</span>
      <span className="top-bar__chevron" aria-hidden="true">
        ▾
      </span>
      <span className={dotClass} aria-hidden="true" />
    </>
  );

  const right = (
    <>
      <Avatar user={user} />
      <span className="top-bar__user">
        <span className="top-bar__user-name">{user.username}</span>
        <span className="top-bar__user-status">
          {online ? "Online" : "Offline"}
        </span>
      </span>
      <span className="top-bar__chevron" aria-hidden="true">
        ▾
      </span>
    </>
  );

  return (
    <header className="top-bar" role="banner">
      {onWorkspaceClick !== undefined ? (
        <button
          type="button"
          className="top-bar__cluster top-bar__cluster--workspace"
          onClick={onWorkspaceClick}
          aria-label={`${workspaceName} workspace`}
        >
          {left}
        </button>
      ) : (
        <div className="top-bar__cluster top-bar__cluster--workspace">{left}</div>
      )}
      <div className="top-bar__center" />
      {onUserClick !== undefined ? (
        <button
          type="button"
          className="top-bar__cluster top-bar__cluster--user"
          onClick={onUserClick}
          aria-label={`${user.username} menu`}
        >
          {right}
        </button>
      ) : (
        <div className="top-bar__cluster top-bar__cluster--user">{right}</div>
      )}
    </header>
  );
}
