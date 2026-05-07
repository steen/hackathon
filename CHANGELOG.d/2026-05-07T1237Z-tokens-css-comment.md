### Changed

- `packages/chat-ui/src/tokens.css`: replace stale `--user-blue/green/purple/yellow` line in the WCAG comment block with a pointer to `colorize.ts`. Those tokens were the pre-Phase-6 four-color palette; the current sender-color path is OKLCH-via-cyrb53 in `colorize.ts`. Comment-only; no token definitions changed.
