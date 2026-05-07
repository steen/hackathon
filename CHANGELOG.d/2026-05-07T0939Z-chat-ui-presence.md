### Changed

- `PresenceList` and `PresenceLiveRegion` extracted into `@hackathon/chat-ui`. ARIA + `data-testid` selectors preserved (`aria-label="Online users"`, `data-testid="presence-list"`, `data-testid="presence-user-${id}"`, `data-testid="presence-live-region"`). The announcement-text useMemo stays in `Chat.tsx` (hook-coupled).
