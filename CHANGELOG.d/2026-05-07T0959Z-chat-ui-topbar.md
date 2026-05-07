### Added

- `TopBar` component in `@hackathon/chat-ui`. Renders the screenshot's top row minus the search box: workspace name + chevron + online dot on the left, initial-letter avatar (deterministic palette via `userColorClass`) + username + status + chevron on the right. Decorative chevrons are inert; the workspace switcher and user popover are explicit follow-ups.
- `apps/web/src/routes/Chat.tsx` wraps the existing layout with `TopBar` + `.chat-layout__body` flex container so the header spans full width above the sidebar + main panels.
