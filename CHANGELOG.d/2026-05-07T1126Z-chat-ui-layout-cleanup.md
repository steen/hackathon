### Removed

- `ConnectionBadge` component (and its CSS, exports, barrel entry) — the TopBar's `role="status"` indicator is now the single source of truth for connection state, so the redundant badge in `ChannelHeader` is gone.
- Sidebar `header` slot and `sidebar__signout` button — the username and Sign-out moved into the TopBar's user cluster. Sidebar is now `<aside>` + children only.
- TopBar workspace chevron and click-handler (`onWorkspaceClick` prop) — no workspace switcher.
- TopBar avatar — unused.
- TopBar workspace-side green dot (was duplicate of the user-side status indicator).
- The "Online"/"Offline" status text in TopBar — replaced by a colored dot (green when connected, red when offline) with the visible label moved to a `.visually-hidden` SR text.
- Phone-width `.messages__header` flex-wrap rules in `apps/web/src/styles.css` — they existed to push the badge below an overflowing title; with the badge gone the rules are dead.
- 2 unit tests in `Chat.test.tsx` that asserted the badge lived inside `.messages__header` and the wrap rules were present.

### Changed

- TopBar exposes an `onSignOut` handler instead of `onUserClick`. Sign-out is now a `.top-bar__signout` button on the right side of the user cluster.
- `Chat.tsx` is shorter — no sidebar-header JSX, no `<ConnectionBadge>` prop wiring on `<ChannelHeader>`.
- `e2e/playwright/web.spec.ts` WS-drops scenario asserts `getByRole("status").toHaveText(/^online$/i)` (was `/^connected$/i`); the role is now owned by the TopBar's status dot.

### Fixed

- TopBar user-cluster vertical alignment: name + sign-out button were ~1px misaligned because the `<button>` has its own line-height. Pinned `line-height: 1` on the cluster + matching vertical padding on the name span.
