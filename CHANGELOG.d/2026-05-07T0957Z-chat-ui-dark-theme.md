### Changed

- Web app switches to the dark Slack-like palette via `@import "../../packages/chat-ui/src/tokens.css"` from `apps/web/src/styles.css`. Component-owned BEM rules (`.sidebar*`, `.messages__*`, `.msg*`, `.composer*`, `.conn*`, `.channels-list*`) move with their components into chat-ui; styles.css keeps only app-level layout (`.chat-layout`, `.error-banner`, `.auth-page`), `.visually-hidden`, focus-visible rules, and the mobile breakpoint.
- `apps/web/src/test-setup.ts` reads chat-ui's component CSS files alongside `styles.css` so jsdom `getComputedStyle` keeps resolving selectors after the move.

### Removed

- `apps/web/src/styles.css` — `:root { --bg, --fg, --muted, --border, --accent, --error, --ok, --warn }` light-mode block; replaced by chat-ui's dark tokens (legacy aliases kept for non-chat surfaces).
