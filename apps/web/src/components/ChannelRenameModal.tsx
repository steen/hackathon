import type * as React from "react";
import { useEffect, useId, useState } from "react";
import { ApiError, type Channel } from "@hackathon/api-client";
import { Modal } from "./Modal.js";
import { CHANNEL_NAME_HELPER_TEXT, isValidChannelName } from "../lib/channelName.js";

interface Props {
  open: boolean;
  onClose: () => void;
  channelId: string | null;
  currentName: string | null;
  onRename: (id: string, name: string) => Promise<Channel>;
}

function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message.length > 0 ? err.message : `Request failed (${String(err.status)})`;
  }
  return "Could not reach the server. Check your connection and try again.";
}

export function ChannelRenameModal(props: Props): React.JSX.Element {
  const { open, onClose, channelId, currentName, onRename } = props;
  const [name, setName] = useState(currentName ?? "");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const helperId = useId();
  const errorId = useId();

  // Pre-fill with the current name each time the modal opens. The parent
  // renders the modal mounted-but-closed across re-renders, so a plain
  // initial-state seed wouldn't refresh when the active channel changes.
  useEffect(() => {
    if (open) {
      setName(currentName ?? "");
      setSubmitting(false);
      setError(null);
    }
  }, [open, currentName]);

  function handleClose(): void {
    if (submitting) return;
    onClose();
  }

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    if (channelId === null || !isValidChannelName(name) || submitting) return;
    if (name === currentName) {
      // No-op rename: just close. The server would 200 with the same row,
      // but a round-trip just to close the modal is wasted work.
      onClose();
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onRename(channelId, name);
      onClose();
    } catch (err) {
      setError(describeError(err));
      setSubmitting(false);
    }
  }

  const valid = isValidChannelName(name);
  const describedBy = error !== null ? `${helperId} ${errorId}` : helperId;

  return (
    <Modal open={open} onClose={handleClose} title="Rename channel">
      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        className="channel-modal-form"
      >
        <label className="channel-modal-form__label">
          <span>New name</span>
          <input
            type="text"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              if (error !== null) setError(null);
            }}
            disabled={submitting}
            autoComplete="off"
            spellCheck={false}
            aria-describedby={describedBy}
            aria-invalid={error !== null}
          />
        </label>
        <p id={helperId} className="channel-modal-form__helper">
          {CHANNEL_NAME_HELPER_TEXT}
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
            {submitting ? "Renaming…" : "Rename"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
