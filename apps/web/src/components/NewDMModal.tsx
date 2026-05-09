import type * as React from "react";
import { useEffect, useState } from "react";
import { ApiError, type Conversation, type User } from "@hackathon/api-client";
import { Modal } from "./Modal.js";
import { getClient } from "../api.js";

// "+ New DM" picker per decision-log L24:
//   - Fetches the cached `/api/users` directory on first open.
//   - Filters out the viewer (self-DM is rejected server-side per §6;
//     filtering client-side avoids surfacing it as a clickable option).
//   - Click on a user → POST /api/dms (idempotent find-or-create) →
//     onCreated() so the parent can switch to the (possibly empty)
//     thread.
//
// The modal uses the Phase 8 #836 Modal primitive; users[] is sorted by
// username so the list reads alphabetically. No search/filter input in
// v1 — at N=100 users the unfiltered list fits comfortably and the spec
// (L24) explicitly defers filtering.

interface Props {
  open: boolean;
  onClose: () => void;
  selfUserId: string | null;
  /** Idempotent find-or-create. Returns the conversation row. */
  onCreate: (peerUserId: string) => Promise<Conversation>;
  /** Called once after a successful create so the parent can switch to
   *  the new conversation. */
  onCreated?: (conversation: Conversation) => void;
}

interface UsersResponse {
  users: User[];
}

function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message.length > 0 ? err.message : `Request failed (${String(err.status)})`;
  }
  return "Could not reach the server. Check your connection and try again.";
}

export function NewDMModal(props: Props): React.JSX.Element {
  const { open, onClose, selfUserId, onCreate, onCreated } = props;
  const [users, setUsers] = useState<User[]>([]);
  const [usersLoading, setUsersLoading] = useState(false);
  const [usersError, setUsersError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const [creatingId, setCreatingId] = useState<string | null>(null);

  // Lazy-load the directory on first open. Refetch on every open so a
  // long-lived tab picks up users that registered since last open.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setUsersLoading(true);
    setUsersError(null);
    setCreateError(null);
    /* eslint-disable @typescript-eslint/no-unnecessary-condition --
       `cancelled` is mutated by the effect-cleanup closure; eslint can't
       see the cross-closure write across the await below. */
    void (async () => {
      try {
        const data = await getClient().http.request<UsersResponse>("GET", "/api/users");
        if (cancelled) return;
        const filtered = data.users
          .filter((u) => u.id !== selfUserId)
          .sort((a, b) => {
            if (a.username === b.username) return a.id < b.id ? -1 : 1;
            return a.username < b.username ? -1 : 1;
          });
        setUsers(filtered);
        setUsersLoading(false);
      } catch (err) {
        if (cancelled) return;
        setUsersError(describeError(err));
        setUsersLoading(false);
      }
    })();
    /* eslint-enable @typescript-eslint/no-unnecessary-condition */
    return () => {
      cancelled = true;
    };
  }, [open, selfUserId]);

  function handleClose(): void {
    if (creatingId !== null) return;
    onClose();
    // Reset transient state so a re-open doesn't surface stale error copy
    // from the previous attempt.
    setCreateError(null);
  }

  async function handleSelect(peerId: string): Promise<void> {
    if (creatingId !== null) return;
    setCreatingId(peerId);
    setCreateError(null);
    try {
      const conv = await onCreate(peerId);
      onClose();
      onCreated?.(conv);
    } catch (err) {
      setCreateError(describeError(err));
    } finally {
      setCreatingId(null);
    }
  }

  return (
    <Modal open={open} onClose={handleClose} title="New direct message">
      <div className="channel-modal-form">
        {usersLoading ? <p>Loading users...</p> : null}
        {usersError !== null ? (
          <p role="alert" className="channel-modal-form__error">
            {usersError}
          </p>
        ) : null}
        {createError !== null ? (
          <p role="alert" className="channel-modal-form__error">
            {createError}
          </p>
        ) : null}
        {!usersLoading && usersError === null && users.length === 0 ? (
          <p>No other users to message yet.</p>
        ) : null}
        <ul aria-label="People" className="channels-list">
          {users.map((u) => (
            <li key={u.id}>
              <button
                type="button"
                onClick={() => {
                  void handleSelect(u.id);
                }}
                disabled={creatingId !== null}
                aria-label={`Direct message ${u.username}`}
              >
                <span className="channels-list__name">{u.username}</span>
                {creatingId === u.id ? <span aria-hidden="true">…</span> : null}
              </button>
            </li>
          ))}
        </ul>
        <div className="channel-modal-form__buttons">
          <button type="button" onClick={handleClose} disabled={creatingId !== null}>
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  );
}
