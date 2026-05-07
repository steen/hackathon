import type * as React from "react";
import { useMemo, useRef, type FormEvent, type KeyboardEvent } from "react";
import { setRef } from "../setRef.js";
import "./MessageComposer.css";

const WARN_RATIO = 0.8;

function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

interface Props {
  value: string;
  onChange: (next: string) => void;
  /** Called when the user commits the draft (Enter or Send-button click).
      The component does NOT trim or clear `value`; the consumer owns those. */
  onSubmit: () => void;
  disabled?: boolean;
  maxBytes: number;
  placeholder?: string;
  composerRef?: React.Ref<HTMLTextAreaElement>;
}

export function MessageComposer({
  value,
  onChange,
  onSubmit,
  disabled,
  maxBytes,
  placeholder,
  composerRef,
}: Props): React.JSX.Element {
  // IME composition flag: Enter committing a candidate must NOT submit.
  const composingRef = useRef(false);
  const internalRef = useRef<HTMLTextAreaElement | null>(null);

  const draftBytes = useMemo(() => byteLength(value), [value]);
  const overCap = draftBytes > maxBytes;
  const showCounter = draftBytes >= Math.floor(maxBytes * WARN_RATIO);
  const trimmedEmpty = value.trim().length === 0;
  const sendDisabled = disabled === true || trimmedEmpty || overCap;

  function commit(): void {
    if (sendDisabled) return;
    onSubmit();
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>): void {
    if (e.key !== "Enter") return;
    if (e.shiftKey) return;
    if (composingRef.current) return;
    if (e.nativeEvent.isComposing) return;
    e.preventDefault();
    commit();
  }

  function onFormSubmit(e: FormEvent<HTMLFormElement>): void {
    e.preventDefault();
    commit();
  }

  function attachRef(el: HTMLTextAreaElement | null): void {
    internalRef.current = el;
    setRef(composerRef, el);
  }

  return (
    <form
      className="composer"
      onSubmit={onFormSubmit}
      aria-describedby={showCounter && !overCap ? "composer-counter" : undefined}
    >
      <textarea
        ref={attachRef}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
        onKeyDown={onKeyDown}
        onCompositionStart={() => {
          composingRef.current = true;
        }}
        onCompositionEnd={() => {
          composingRef.current = false;
        }}
        placeholder={placeholder}
        disabled={disabled === true}
        aria-label="message"
        aria-invalid={overCap ? "true" : undefined}
        aria-errormessage={overCap ? "composer-counter" : undefined}
        rows={2}
        data-testid="composer-textarea"
      />
      <button type="submit" disabled={sendDisabled} className="composer__send">
        Send
      </button>
      {showCounter ? (
        <span
          id="composer-counter"
          className={
            overCap
              ? "composer__counter composer__counter--error"
              : "composer__counter composer__counter--warn"
          }
          role="status"
          data-testid="composer-counter"
        >
          {draftBytes} / {maxBytes} bytes
          {overCap ? " — too long to send" : ""}
        </span>
      ) : null}
    </form>
  );
}
