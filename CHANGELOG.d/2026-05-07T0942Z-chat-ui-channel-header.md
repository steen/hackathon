### Changed

- `ChannelHeader` extracted from `apps/web/src/routes/Chat.tsx` into `@hackathon/chat-ui`. Renders `# {channel}` (or `Select a channel`), `ConnectionBadge`, and forwards an optional `headingRef` for the focus-on-channel-switch effect that still lives in Chat.tsx.
