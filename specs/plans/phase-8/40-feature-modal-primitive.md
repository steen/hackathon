# Feature: Web modal primitive

**Parent phase:** Phase 8 — Channel lifecycle (create + rename)
**Status:** planned

## Background

`apps/web` has no shared modal/dialog component. Phase 8 introduces two flows that need one — "create channel" and "rename channel" (see `60-feature-channel-create-rename-ui.md`). Without a shared primitive, each flow either inlines its own overlay (two near-duplicate implementations that drift) or grows ad-hoc styles into `apps/web/src/styles.css` until the next feature inherits a half-baked dialog.

We deliberately keep the primitive minimal: open / close, escape-to-close, click-outside-to-close, focus trap on open, focus-restore to the trigger on close. No portal manager, no animation library, no stacked-modal logic. If a third use case appears post-Phase-8, we revisit then.

## Goal

A single `<Modal>` React component that wraps modal content with consistent overlay, focus management, and dismiss behavior, used by every Phase 8 web modal.

## Approach

1. New file: `apps/web/src/components/Modal.tsx`. Pure component, no context. Props:
   - `open: boolean`
   - `onClose: () => void`
   - `title: string` (rendered in an `h2` with `id` referenced by `aria-labelledby`)
   - `children: ReactNode` (modal body)
   - Optional `initialFocusRef?: RefObject<HTMLElement>` for forms that want a specific input focused on open.
2. Render via `createPortal` into `document.body` to escape stacking-context bugs in the layout grid.
3. ARIA: `role="dialog"`, `aria-modal="true"`, `aria-labelledby` pointing at the title's id. Keyboard: Escape closes; Tab cycles within the modal (focus trap); on close, restore focus to the previously focused element.
4. Click-outside: pointerdown on the overlay (not on the modal panel) calls `onClose`. Use `pointerdown` rather than `click` so dragging a selection out of the modal does not dismiss.
5. Styling: extend `apps/web/src/styles.css` with overlay + panel rules. No new CSS file, no Tailwind, per PRD §"Design deviations" — Context + plain CSS at this scale.
6. No external dependencies. Implement focus trap by hand (~30 LOC); pulling in `focus-trap-react` adds a runtime dep we do not need at one-modal-per-flow scale.

## Acceptance criteria

- Modal renders nothing when `open === false`; mounts into `document.body` (not the React tree's parent) when `open === true`.
- Escape key triggers `onClose`.
- Pointerdown on the overlay (outside the panel) triggers `onClose`; pointerdown inside the panel does not.
- On open, focus moves into the modal — to `initialFocusRef.current` if provided, otherwise to the first focusable element in the panel.
- On close, focus returns to the element that was focused when the modal opened.
- Tab from the last focusable element wraps to the first; Shift+Tab from the first wraps to the last.
- Modal has `role="dialog"`, `aria-modal="true"`, and `aria-labelledby` correctly bound to the title `h2`.

## Out of scope

- Stacked modals (only one at a time in Phase 8).
- Animations / transitions.
- A confirm/alert variant — flows that need confirmation render their own buttons inside the modal.
- A `Drawer` or sheet variant.

## Pointers

- `apps/web/src/components/` — existing component conventions (verify the directory exists; `apps/web/src/` layout has been moving).
- `apps/web/src/styles.css` — single project stylesheet.
- WAI-ARIA Authoring Practices "Dialog (Modal)" pattern — the focus + ARIA shape this primitive mirrors.
