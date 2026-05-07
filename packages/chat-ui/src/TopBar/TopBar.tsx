import type * as React from "react";
import "./TopBar.css";

interface TopBarUser {
  id: string;
  username: string;
}

interface Props {
  workspaceName: string;
  user: TopBarUser;
  /** Whether the WS connection to the chat server is established. Drives
      the user-cluster status dot (green when online, red when offline). */
  online: boolean;
  /** Sign-out handler. Wired to a button in the user cluster; replaces
      the previous sidebar-header `Sign out` button so the user identity
      lives in exactly one place. */
  onSignOut: () => void;
}

export function TopBar({ workspaceName, user, online, onSignOut }: Props): React.JSX.Element {
  const dotClass = online
    ? "top-bar__status-dot top-bar__status-dot--online"
    : "top-bar__status-dot top-bar__status-dot--offline";
  const statusText = online ? "online" : "offline";

  return (
    <header className="top-bar" role="banner">
      <div className="top-bar__cluster top-bar__cluster--workspace">
        <span className="top-bar__hash" aria-hidden="true">
          #
        </span>
        <span className="top-bar__workspace-name">{workspaceName}</span>
      </div>
      <div className="top-bar__center" />
      <div className="top-bar__cluster top-bar__cluster--user">
        {/* Single source of truth for connection status visible to the user.
            role="status" + aria-live="polite" so screen readers announce
            online/offline transitions. Tests querying `getByRole("status")`
            (web.spec.ts WS-drops scenario) find exactly one match. The
            visual rendering is a colored dot; the text is visually hidden
            so the SR announcement still works. */}
        <span className={dotClass} role="status" aria-live="polite">
          <span className="visually-hidden">{statusText}</span>
        </span>
        <span className="top-bar__user-name">{user.username}</span>
        <button
          type="button"
          className="top-bar__signout"
          onClick={onSignOut}
          aria-label="Sign out"
        >
          Sign out
        </button>
      </div>
    </header>
  );
}
