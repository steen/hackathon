### Fixed

- `Modal` now portals into `document.body` via `createPortal` so the dialog escapes any ancestor stacking context (transform / filter / position: fixed) — closes the spec-AC drift introduced in PR #878.
- Backdrop dismiss switched from `click` to `pointerdown` per the feature spec so dragging a text selection out of the panel and releasing on the backdrop no longer dismisses. Panel-internal pointerdown stops propagation, preserving the asymmetry the spec calls out.

### Tests

- Added Vitest coverage for the Tab / Shift-Tab focus-trap branches (last → first, first → last, outside-panel snap-back) so future refactors of the trap can't silently drop the wrap-around without failing CI.
- Added a portal-mount assertion plus a `pointerdown-on-panel + pointerup-on-backdrop = no dismiss` regression case.
