import type * as React from "react";
import "./PresenceList.css";

export interface PresenceUser {
  id: string;
  username: string;
}

interface Props {
  users: PresenceUser[];
}

export function PresenceList({ users }: Props): React.JSX.Element {
  return (
    <ul className="presence" aria-label="Online users" data-testid="presence-list">
      {users.map((u) => (
        <li key={u.id} data-testid={`presence-user-${u.id}`}>
          {u.username.length > 0 ? u.username : u.id}
        </li>
      ))}
    </ul>
  );
}
