### Fixed

- **Login + register screens unreadable on dark theme**. `apps/web/src/styles.css` had a `:root` block with self-references (`--fg: var(--fg)`) that resolved to invalid → black-on-black. The aliases are removed; `.auth-page` rules now use chat-ui tokens directly. Also restyles inputs, submit button, and footer text/link to dark theme.
- **`.linklike` button rendered ~13px** because `<button>`s default to a smaller font and the class lacked `font: inherit`. "No account? Register" now reads as one line at the same font size.
- **Sign-out button matched browser default**. Now styled as a tertiary action (`.sidebar__signout`): transparent bg, muted text, hover swap.
- **Horizontal alignment of usernames and message bodies**. Time slot is now a fixed-width column (CSS custom property `--msg-col-time`) rather than `min-width`, so the body's left edge precisely matches where the username starts on the meta line above.
- **Color-contrast regressions**. WCAG AA audit ran against every fg/bg pair; bumped `--fg-muted` (#8b8b94 → #9ba0aa, 5.71 → 7.35 on `--bg-app`), `--fg-subtle` (#6b6b73 → #a1a1aa, 3.65 → 7.52), introduced `--accent-text` (#a78bfa) for accent-as-text usages so the accent button surface keeps the brighter `--accent` while body links stay ≥4.5:1.

### Added

- **`GET /api/users`** — server endpoint returning every registered user as `{id, username}`. The web client fetches it on mount in parallel with `/api/presence` and merges into the username directory so senders who have since gone offline still resolve to their username (previously rendered as a raw ULID).
- `--accent-text` token for accent-colored text on dark surfaces (separate from `--accent` which keeps its role as button surface).
- Per-message column tokens (`--msg-col-time`, `--msg-col-gap`) so the meta-line and body share the same horizontal anchor.

### Changed

- HTML root font-size set to `14px` so `1rem = 14px`. Tightens chat density to match the reference screenshot. All component CSS uses rem and stays responsive to root sizing.
- Typography pass across Sidebar, ChannelsList, PresenceList, ChannelHeader, MessageList, MessageItem, MessageComposer, TopBar to fit a coherent scale.
