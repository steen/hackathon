import type * as React from "react";
import { useId, useState } from "react";
import { ApiError, type ChannelMember } from "@hackathon/api-client";
import { Modal } from "./Modal.js";

interface Props {
  open: boolean;
  onClose: () => void;
  /** Inviter callback: parent issues the actual API call so the modal
   *  stays free of the auth-state plumbing. The modal validates that
   *  the user id field is non-empty before invoking the callback. */
  onInvite: (userId: string) => Promise<ChannelMember>;
  /** Called once after a successful invite so the parent can refresh
   *  the panel without a full reload round-trip. */
  onInvited?: (member: ChannelMember) => void;
}

function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message.length > 0 ? err.message : `Request failed (${String(err.status)})`;
  }
  return "Could not reach the server. Check your connection and try again.";
}

export function ChannelInviteModal(props: Props): React.JSX.Element {
  const { open, onClose, onInvite, onInvited } = props;
  const [userId, setUserId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const helperId = useId();
  const errorId = useId();

  function reset(): void {
    setUserId("");
    setSubmitting(false);
    setError(null);
  }

  function handleClose(): void {
    if (submitting) return;
    reset();
    onClose();
  }

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    if (userId.trim().length === 0 || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const member = await onInvite(userId.trim());
      reset();
      onClose();
      onInvited?.(member);
    } catch (err) {
      setError(describeError(err));
      setSubmitting(false);
    }
  }

  const valid = userId.trim().length > 0;
  const describedBy = error !== null ? `${helperId} ${errorId}` : helperId;

  return (
    <Modal open={open} onClose={handleClose} title="Invite member">
      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        className="channel-modal-form"
      >
        <label className="channel-modal-form__label">
          <span>User id</span>
          <input
            type="text"
            value={userId}
            onChange={(e) => {
              setUserId(e.target.value);
              if (error !== null) setError(null);
            }}
            disabled={submitting}
            autoComplete="off"
            spellCheck={false}
            aria-describedby={describedBy}
            aria-invalid={error !== null}
            placeholder="01HZZ…"
          />
        </label>
        <p id={helperId} className="channel-modal-form__helper">
          Paste the recipient&rsquo;s user id (visible in their profile or via
          <code> /api/users</code>).
        </p>
        {error !== null ? (
          <p id={errorId} role="alert" className="channel-modal-form__error">
            {error}
          </p>
        ) : null}
        <div className="channel-modal-form__buttons">
          <button type="button" onClick={handleClose} disabled={submitting}>
            Cancel
          </button>
          <button type="submit" disabled={!valid || submitting}>
            {submitting ? "Inviting…" : "Invite"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
