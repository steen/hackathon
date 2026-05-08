### Added

- `apps/web/src/components/Modal.tsx`: accessible modal/dialog primitive (#836). `role="dialog"` + `aria-modal` + `aria-labelledby` on a generated title id; Escape and backdrop-click close; clicks inside the panel do not bubble; focus trap with first-focusable initial focus and trigger-restore on close; `document.body` overflow lock toggled across the open lifecycle. Below 767px (the existing mobile sidebar breakpoint) the panel fills the viewport. No consumers wired in this PR — channel-create and channel-rename ship the wiring on their own tickets.
