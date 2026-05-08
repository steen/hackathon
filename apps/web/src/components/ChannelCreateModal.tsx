import type * as React from "react";
import { useId, useState } from "react";
import { ApiError, type Channel } from "@hackathon/api-client";
import { Modal } from "./Modal.js";
import { CHANNEL_NAME_HELPER_TEXT, isValidChannelName } from "../lib/channelName.js";

interface Props {
  open: boolean;
  onClose: () => void;
  onCreate: (name: string) => Promise<Channel>;
  /** Called once after a successful create so the parent can switch focus
   *  to the new channel. */
  onCreated?: (ch: Channel) => void;
}

// Pulls the inline error copy from the server's structured envelope.
// Anything else (network error, 5xx with empty body) falls back to a
// short generic line; details land on console via the api-client.
function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message.length > 0 ? err.message : `Request failed (${String(err.status)})`;
  }
  return "Could not reach the server. Check your connection and try again.";
}

export function ChannelCreateModal(props: Props): React.JSX.Element {
  const { open, onClose, onCreate, onCreated } = props;
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const helperId = useId();
  const errorId = useId();

  function reset(): void {
    setName("");
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
    if (!isValidChannelName(name) || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const ch = await onCreate(name);
      // Success: close, reset, hand off to the parent.
      reset();
      onClose();
      onCreated?.(ch);
    } catch (err) {
      setError(describeError(err));
      setSubmitting(false);
    }
  }

  const valid = isValidChannelName(name);
  const describedBy = error !== null ? `${helperId} ${errorId}` : helperId;

  return (
    <Modal open={open} onClose={handleClose} title="Create channel">
      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        className="channel-modal-form"
      >
        <label className="channel-modal-form__label">
          <span>Channel name</span>
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
            placeholder="books"
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
            {submitting ? "Creating…" : "Create"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
