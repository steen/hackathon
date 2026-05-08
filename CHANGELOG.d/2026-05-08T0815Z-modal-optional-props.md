### Added

- `apps/web/src/components/Modal.tsx`: two additive opt-in props on `ModalProps` for consumer flexibility (#882). `closeOnBackdrop?: boolean` (default `true`, preserves current behavior) lets destructive-confirm flows make backdrop clicks a no-op while Escape still closes. `initialFocusRef?: React.RefObject<HTMLElement | null>` directs initial focus to a specific element inside the panel; the unset / null / out-of-panel cases fall back to the first-focusable behavior.
