import type * as React from "react";
import { useEffect, useState } from "react";
import { ApiError, type ChannelMember } from "@hackathon/api-client";
import { getClient } from "../api.js";

interface Props {
  /** Channel id whose members to render. Empty string is treated as
   *  "no channel selected" — the panel shows an inert placeholder. */
  channelId: string;
  /** Optional invite handler. When undefined, the panel hides the
   *  invite controls; when present, the parent owns the invite flow
   *  (a modal, a user-picker, etc.) and the panel only surfaces the
   *  trigger. */
  onInvite?: () => void;
}

function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message.length > 0 ? err.message : `Request failed (${String(err.status)})`;
  }
  return "Could not reach the server.";
}

export function ChannelMembersPanel(props: Props): React.JSX.Element {
  const { channelId, onInvite } = props;
  const [members, setMembers] = useState<ChannelMember[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (channelId === "") {
      setMembers([]);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    getClient()
      .listChannelMembers(channelId)
      .then((list) => {
        if (cancelled) return;
        setMembers(list);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setMembers([]);
        setError(describeError(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [channelId]);

  if (channelId === "") {
    return (
      <aside className="channel-members-panel" aria-label="Channel members">
        <p className="channel-members-panel__placeholder">Select a channel to see members.</p>
      </aside>
    );
  }

  return (
    <aside className="channel-members-panel" aria-label="Channel members">
      <header className="channel-members-panel__header">
        <h3>Members</h3>
        {onInvite !== undefined ? (
          <button type="button" onClick={onInvite}>
            Invite
          </button>
        ) : null}
      </header>
      {loading ? <p className="channel-members-panel__status">Loading…</p> : null}
      {error !== null ? (
        <p role="alert" className="channel-members-panel__error">
          {error}
        </p>
      ) : null}
      <ul className="channel-members-panel__list">
        {members.map((m) => (
          <li key={m.user_id} className="channel-members-panel__item">
            <span className="channel-members-panel__username">
              {m.username !== undefined && m.username !== "" ? m.username : m.user_id}
            </span>
          </li>
        ))}
      </ul>
    </aside>
  );
}
