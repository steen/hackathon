### Added

- New workspace package `@hackathon/chat-ui` with dark-theme tokens (`tokens.css`) and React 18.3.1 peer-dep parity. Exports source-only (`./src/index.ts`) per workspace convention; `tokens.css` exposed via `"./tokens.css"` exports entry.
- `apps/web` resolves the new package via `tsconfig.paths`, `vite.config.alias`, `vitest.config.alias` (defensive parity with `@hackathon/api-client`).

### Changed

- `scripts/check-workspace-exports.mjs` widens the source-path check to allow `.css` files in `exports` maps; required so chat-ui's `tokens.css` entry passes the workspace-exports gate.
