### Added

- `packages/chat-ui/src/styles.css` — single barrel stylesheet that `@import`s every component CSS file plus the design tokens. Exposed via the package's `exports["./styles.css"]` entry. `apps/web/src/styles.css` and `apps/web/src/test-setup.ts` consume the barrel; consumers no longer bookkeep the per-component path list.
- `packages/chat-ui` exports `MESSAGE_MAX_BYTES` (4096). The constant mirrors the server's `MaxMessageBodyBytes`; `Chat.tsx` imports it instead of redefining the same number.

### Changed

- `apps/web/src/hooks/usePresence.ts` no longer defines `PresenceUser`; the type is owned by `@hackathon/chat-ui`'s `types.ts`. The hook re-exports it for callers that previously imported from this module.
- `apps/web/src/test-setup.ts` reads the chat-ui CSS barrel rather than enumerating every component stylesheet path. Adding a new component's CSS to the bundle is now a one-line edit in chat-ui's `styles.css`.

### Fixed

- Off-by-one in the relative path of `apps/web/src/styles.css`'s `@import` of chat-ui CSS (was `../../packages/...` from `apps/web/src/`, which Vite resolved through some other mechanism but Node's `path.resolve` did not). Corrected to `../../../packages/...`.
