import type * as React from "react";
import { useCallback, useEffect, useId, useRef } from "react";

// Accessible modal primitive. No consumers in this PR — wired by the
// channel-create / channel-rename flows in their own tickets ("introduce
// dead code" pattern). Behavior contract:
//   - role=dialog + aria-modal + aria-labelledby pointing at the title <h2>.
//   - Focus moves to the first focusable element inside the panel on open;
//     focus is restored to the previously-focused element on close.
//   - Tab/Shift-Tab cycle within the panel (focus trap).
//   - Escape closes; backdrop click closes; clicks inside the panel do not
//     bubble out and trigger the backdrop close.
//   - document.body scroll lock while open; the prior overflow value is
//     restored on close/unmount.

const FOCUSABLE_SELECTOR = [
  "a[href]",
  "area[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "button:not([disabled])",
  "iframe",
  "object",
  "embed",
  "[contenteditable=true]",
  '[tabindex]:not([tabindex="-1"])',
].join(",");

function getFocusable(root: HTMLElement): HTMLElement[] {
  const nodes = root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
  return Array.from(nodes).filter((el) => !el.hasAttribute("disabled") && el.tabIndex !== -1);
}

export interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
}

export function Modal(props: ModalProps): React.JSX.Element | null {
  const { open, onClose, title, children } = props;
  const panelRef = useRef<HTMLDivElement | null>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);
  const titleId = useId();

  // Latest onClose without forcing the keydown effect to re-run when the
  // caller passes a fresh closure each render.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  // Capture trigger focus + apply scroll lock + initial focus when opening;
  // restore both when closing or unmounting.
  useEffect(() => {
    if (!open) return;

    previouslyFocusedRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    const priorOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    const panel = panelRef.current;
    if (panel !== null) {
      const focusable = getFocusable(panel);
      const target = focusable[0] ?? panel;
      target.focus();
    }

    return () => {
      document.body.style.overflow = priorOverflow;
      const prev = previouslyFocusedRef.current;
      if (prev !== null && document.contains(prev)) {
        prev.focus();
      }
      previouslyFocusedRef.current = null;
    };
  }, [open]);

  // Escape + Tab trap.
  useEffect(() => {
    if (!open) return;
    function onKeyDown(e: KeyboardEvent): void {
      if (e.key === "Escape") {
        e.preventDefault();
        onCloseRef.current();
        return;
      }
      if (e.key !== "Tab") return;
      const panel = panelRef.current;
      if (panel === null) return;
      const focusable = getFocusable(panel);
      if (focusable.length === 0) {
        e.preventDefault();
        panel.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (first === undefined || last === undefined) return;
      const active = document.activeElement;
      if (e.shiftKey) {
        if (active === first || !panel.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (active === last || !panel.contains(active)) {
          e.preventDefault();
          first.focus();
        }
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const onBackdropClick = useCallback(() => {
    onCloseRef.current();
  }, []);

  const onPanelClick = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation();
  }, []);

  if (!open) return null;

  return (
    <div className="modal-backdrop" data-testid="modal-backdrop" onClick={onBackdropClick}>
      <div
        ref={panelRef}
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        onClick={onPanelClick}
      >
        <h2 id={titleId} className="modal-panel__title">
          {title}
        </h2>
        <div className="modal-panel__body">{children}</div>
      </div>
    </div>
  );
}
